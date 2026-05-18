# UKPS Codebase Guide for AI Agents

## Repository Overview

```
ukps/                          # Root — UK Payment Systems (uni project)
├── AGENTS.md                  # ← This file
├── .gitignore
├── bacs-service/              # EMPTY — BACS (Standard 18), batch settlement
├── fps-service/               # EMPTY — FPS (ISO 20022 + ISO 8583), near-real-time
├── chaps-service/             # IMPLEMENTED — CHAPS (ISO 20022), RTGS, Go + React
```

Three services mimic the UK interbank payment network. Only `chaps-service` has code; the other two are design-ready stubs.

---

## chaps-service — Reference Implementation

The most important codebase for patterns to copy.

### Entry Point
`cmd/server/main.go` — wires DB pool → validator registry → server → HTTP listener.

### Package Layout (Go standard layout)

| Directory | Responsibility |
|---|---|
| `cmd/server/` | Single `main.go` — bootstrap only |
| `pkg/server/` | HTTP router + 17 handlers, JSON/XML encoding |
| `pkg/ledger/` | Business logic: settlement, participants, positions, limits, liquidity |
| `pkg/iso20022/` | XML struct models for pacs.008, pacs.002, Business Application Header |
| `pkg/validator/` | XSD schema registry + envelope validation via libxml2 |
| `internal/db/` | SQL migrations: `01_init.sql` (schema), `02_seed.sql` (4 banks) |
| `web/chaps-gui/` | React 18 + Vite 5 + TypeScript 5 operator dashboard |
| `xsd/` | ISO 20022 XSD files (pacs.008, pacs.002, head.001, chaps_wrapper) |
| `test/` | Sample XML payloads for manual testing |

### Key Architectural Patterns

#### 1. Server struct composition
```go
type Server struct {
    Validator *validator.ValidatorRegistry
    Ledger    *ledger.LedgerService
}
```
Routes registered via `RegisterRoutes(mux *http.ServeMux)` using Go 1.22+ pattern syntax.

#### 2. Go 1.22+ routing
```go
mux.HandleFunc("POST /v1/payments/chaps/{id}/authorize", handler)
// Access path params with: r.PathValue("id")
```

#### 3. Content-type dispatch
`ProcessPayment` inspects `Content-Type`:
- `application/json` → `processJSONPayment` (GUI-originated)
- `application/xml` → validate XSD → unmarshal → settle → return pacs.002

#### 4. Idempotent settlement
```sql
INSERT INTO transactions (msg_id, ...) VALUES (...)
ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
RETURNING id, status
```
If status is already `SETTLED`, return cached `ACTC` result.

#### 5. Normalized DB schema (5 tables)
- `participant_profiles` — static BIC/name/currency
- `participant_liquidity` — high-frequency balance updates
- `participant_statuses` — ACTIVE/SUSPENDED/DISABLED + block info
- `transactions` — payment records, UUID v7 primary key
- `journal_entries` — immutable audit trail with `pg_notify` trigger

#### 6. ISO 20022 message flow
```
Inbound XML (pacs.008 wrapped in BizMsg)
  → chaps_wrapper.xsd validation
  → XPath extract MsgDefIdr + Document
  → xml.Unmarshal into Pacs008Message struct
  → Ledger.SettlePayment (DB tx with FOR UPDATE row locks)
  → Generate Pacs002Message (ACTC/RJCT/PDNG)
  → Wrap in BusinessMessage{AppHdr + Document}
  → Return XML
```

#### 7. psql NOTIFY for real-time
A trigger on `journal_entries` fires `pg_notify('liquidity_event', account_bic)` on credit entries.

---

## Conventions to Follow When Extending

### Adding a new payment scheme (e.g. FPS, BACS)
1. Create `{scheme}-service/` at repo root
2. Create `cmd/server/main.go` — bootstrap pattern (DB → validator → server)
3. Create `pkg/server/server.go` — register scheme-specific routes
4. Create `pkg/ledger/service.go` — settlement logic
5. Create `pkg/{format}/` for message models
6. Create `internal/db/` for migrations
7. Create `Dockerfile`, `compose.yml`, `compose-dev.yml`
8. Use `/v1/payments/{scheme}/...` for payments, `/v1/participants/...` for participants
9. Use same `participant_profiles` table or shared participant registry across services

### Adding a new message format
1. Create `pkg/{formatname}/` package
2. Define Go structs with `xml:"..."` tags
3. Register XSD in `main.go` via `registerXSD(reg, "schema_name")`
4. Add handler in `server.go` that content-type dispatches to the right format handler

### Adding a new XSD
1. Add `.xsd` file to `xsd/` directory  
2. Register in `main.go`: `registerXSD(reg, "filename_without_ext")`
3. Refer to existing `chaps_wrapper.xsd` for envelope pattern

### API style
- Paths: `/v1/{resource}/{scheme}[/{id}[/action]]`
- JSON for GUI/admin, XML for ISO 20022 external messages
- Error responses: `{"error": "message"}`
- HTTP status codes: 200 (success), 201 (created), 202 (accepted), 400 (bad request), 404 (not found), 409 (conflict), 500 (internal error), 503 (unavailable)

### Database conventions
- Use `DECIMAL(20, 2)` for monetary amounts
- Use native `uuidv7()` for UUID primary keys (Postgres 18)
- Separate tables for profile, liquidity, status (normalized, different update frequencies)
- Use `ON CONFLICT` for idempotency
- Use `FOR UPDATE` row locks in settlement transactions
- Prefix transaction tables with `transactions` and audit with `journal_entries`

### Go conventions used
- Standard library `net/http` (no third-party router)
- `pgx/v5` with `pgxpool` for connection pooling
- `pgx.BeginFunc` for transactional logic
- `log.Printf` for logging (no structured logging yet)
- `encoding/xml` + `encoding/json` for serialization
- Package name matches directory name
- Error sentinel values: `var ErrX = errors.New("...")`

### Frontend conventions
- React 18 with TypeScript, plain CSS (no CSS framework)
- Vite dev proxy: `/v1` → `localhost:8080`
- API client in `api.ts` with generic `request<T>()` wrapper
- Types in `types.ts` matching Go struct JSON tags
- Polish language UI labels

### Docker conventions
- Multi-stage build: `golang:1.26-alpine` → `alpine:3.23`
- Static link libxml2 with CGO
- Port 8080
- DB runs in separate container (Postgres 18-alpine)
- `compose.yml` for production, `compose-dev.yml` for dev (DB only)

### Testing
- No test files exist yet. When adding tests:
  - Go: `_test.go` files alongside source with `package X_test`
  - Frontend: Vitest or React Testing Library
  - SQL: use Docker compose-dev + manual seed verification

### Git conventions
- `.gitignore` ignores `node_modules/`, `dist/`, `.vite/`, `*.log`, `.env`
- No `Thumbs.db`, `.DS_Store`, `.vscode/`
- No secrets in code — use env vars or defaults for dev only

---

## Important Gotchas

1. **Route ordering matters**: `/v1/payments/chaps/validate` must be registered **before** `/v1/payments/chaps/{id}` or Go 1.22 mux will match `{id}` = "validate". Look at `RegisterRoutes` — validate is listed before the `{id}` routes.
2. **CGO is required**: libxml2 bindings use CGO. Build with musl tags for Alpine.
3. **Database URL forces TCP**: Default `DATABASE_URL` uses `127.0.0.1` instead of `localhost` to avoid Unix socket ambiguity.
4. **Postgres 18 specific**: uses `uuidv7()` function not present in older versions.
5. **No auth**: Authorization endpoint is a stub. No real 2FA or digital signature verification.
6. **`xsd/chaps_wrapper.xsd`** is a *custom* envelope — not standard ISO 20022. It wraps `AppHdr` + `Document` for single-XSD validation.
7. **pacs.009 and pacs.029 XSDs** are included but unused — available for extension (bank-to-bank transfers, investigation messages).

---

## How This Fits Together: UKPS Architecture

```
┌────────────────────┐     ┌────────────────────┐     ┌────────────────────┐
│   bacs-service     │     │    fps-service      │     │   chaps-service    │
│   (Standard 18)    │     │ (ISO20022+ISO8583)  │     │   (ISO 20022)      │
│   Batch / 3-day    │     │   Near-real-time    │     │   RTGS / High-val  │
└────────┬───────────┘     └────────┬────────────┘     └────────┬───────────┘
         │                          │                          │
         └──────────────────────────┼──────────────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │        PostgreSQL 18            │
                    │  (shared participants,          │
                    │   separate transaction tables)  │
                    └────────────────────────────────┘
```

Each service is an independent Go binary + optional React GUI, deployable together via Docker Compose. Services share a participant registry conceptually but keep their own transaction tables.
