# UKPS Codebase Guide for AI Agents

## Repository Overview

```
ukps/                          # Root — UK Payment Systems (uni project)
├── AGENTS.md                  # ← This file
├── .gitignore
├── bacs-service/              # IMPLEMENTED — BACS (Standard 18), batch settlement, Go backend
├── fps-service/               # IMPLEMENTED — FPS (ISO 20022 + ISO 8583), near-real-time, Go backend
├── chaps-service/             # IMPLEMENTED — CHAPS (ISO 20022), RTGS, Go + React
```

Three services mimic the UK interbank payment network. All three services have fully implemented Go backends with real business logic.

---

## Service Port Map

| Service | Port | DB Port | DB Name | Other |
| :--- | :--- | :--- | :--- | :--- |
| CHAPS | 8080 | 5432 | `chaps_ledger` | — |
| FPS | 8081 | 5433 | `fps_ledger` | TCP `:7421` (ISO 8583 socket) |
| BACS | 8082 | 5434 | `bacs_ledger` | — |

---

## chaps-service — Reference Implementation

The most important codebase for patterns to copy.

### Entry Point
`cmd/server/main.go` — wires DB pool → validator registry → server → HTTP listener.

### Package Layout (Go standard layout)

| Directory | Responsibility |
|---|---|
| `cmd/server/` | Single `main.go` — bootstrap only |
| `pkg/server/` | HTTP router + 18 handlers, JSON/XML encoding, SSE streaming, background scheduler |
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
    Events    *events.EventBus
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
- `participant_profiles` — static BIC/name/currency/sort_code (UK bank sort code, optional)
- `participant_liquidity` — high-frequency balance updates
- `participant_statuses` — ACTIVE/SUSPENDED/DISABLED + block info
- `transactions` — payment records, UUID v7 primary key, includes optional sender/receiver sort codes
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
9. Use the same normalized schema pattern (3 participant tables per database) — each service maintains its own tables in its own database

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
- SSE: `GET /v1/payments/{scheme}/incoming/{bic}` for real-time payment events
- Error responses: `{"error": "message"}`
- HTTP status codes: 200 (success), 201 (created), 202 (accepted), 400 (bad request), 404 (not found), 409 (conflict), 500 (internal error), 503 (unavailable)

### Database conventions
- Use `DECIMAL(20, 2)` for monetary amounts
- Use native `uuidv7()` for UUID primary keys (Postgres 18)
- Separate tables for profile, liquidity, status (normalized, different update frequencies)
- Use `ON CONFLICT` for idempotency
- Use `FOR UPDATE` row locks in settlement transactions
- Prefix transaction tables with `transactions` and audit with `journal_entries`
- Each service maintains its own database with its own tables — no shared tables across services

### Go conventions used
- Standard library `net/http` (no third-party router)
- `pgx/v5` with `pgxpool` for connection pooling
- `pgx.BeginFunc` for transactional logic
- `log.Printf` for logging (no structured logging yet)
- `encoding/xml` + `encoding/json` for serialization
- Package name matches directory name
- Error sentinel values: `var ErrX = errors.New("...")`
- `pkg/events/events.go` — in-memory EventBus for SSE real-time notifications

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
- Test files exist for ISO 8583 parser, ISO 20022 serialization, and Standard 18 parser:
  - `chaps-service/pkg/iso20022/serialization_test.go` — 17 tests (includes pacs.008 with sort code XML parsing, without sort codes)
  - `chaps-service/pkg/ledger/service_test.go` — 12 tests (includes sort code struct fields)
  - `chaps-service/pkg/server/server_test.go` — 14 tests (includes JSON sort code and register sort code tests)
  - `fps-service/pkg/iso8583/message_test.go` — 7 tests (short msg, MTI validation, optional fields, amount+trace, 0210 encode, round-trip, full field parse)
  - `fps-service/pkg/iso20022/serialization_test.go` — 14 tests (includes pacs.008 sort code XML parsing)
  - `fps-service/pkg/server/server_test.go` — 4 tests (JSON sort code and register sort code tests)
  - `bacs-service/pkg/standard18/parser_test.go` — 13 tests (basic file, AUDDIS, CRLF, validation, pence conversion, multiple records, line padding, zero values)
- Integration smoke test: `test/integration_test.sh` — starts DBs via `compose-dev.yml`, builds & runs services, runs HTTP smoke tests (participants, cycles, etc.)
- When adding tests:
  - Go: `_test.go` files alongside source with `package X_test`
  - Frontend: Vitest or React Testing Library
  - SQL: use Docker compose-dev + manual seed verification

### SSE real-time events

All three services support server-sent events for real-time payment notifications.

| Service | Endpoint | Published On | Event Type |
|---|---|---|---|
| CHAPS | `GET /v1/payments/chaps/incoming/{bic}` | Each `ACTC` settlement | `payment.received` |
| FPS | `GET /v1/payments/fps/incoming/{bic}` | Each `ACTC` settlement | `payment.received` |
| BACS | `GET /v1/payments/bacs/incoming/{bic}` | Cycle `SettleCycle` call | `cycle.settled` |

- Uses in-memory `pkg/events.EventBus` (map of BIC → channel fan-out)
- `NewEventBus()` → `Publish(bic, event)` / `Subscribe(bic, buf)` / `PublishToAll(bics, event)`
- SSE payload: `data: {"type":"payment.received","data":{...}}\n\n`
- Buffered channels (100 events), drops on overflow
- Client disconnect detected via `r.Context().Done()`

### Automated background processing (scheduler)

All three services run a background scheduler driven by `config/sessions.json`. The scheduler uses a **polling tick** — a `time.Ticker` fires every N seconds, wakes up, queries the DB for work, executes it, then sleeps until the next tick.

#### How the tick works (scheduler loop)

```
StartScheduler:
  1. Load config from sessions.json
  2. Compute interval = min(demo_session_minutes, 60) seconds (demo) or 60s (production)
  3. Create time.Ticker(interval)
  4. Loop:
     - Wait for tick or context cancellation
     - On tick: query DB for work, execute, log results
     - On cancel: stop ticker, return
```

The tick interval is a trade-off: shorter = more responsive but more DB queries; longer = less load but delayed reactions. 60s is the default; `demo_session_minutes` speeds it up for demos.

#### Config-driven behavior (`config/sessions.json`)

```
config/sessions.json
├── mode               "demo" or "production" — explicit switch
├── demo_session_minutes  Accelerated time window (e.g. 15 min = 15s tick)
├── FPS-specific
│   ├── settlement_times    Wall-clock settlement windows (e.g. 09:00, 15:00)
│   └── opening/closing_time  Enforced — ProcessPayment and handleISO8583TCP reject outside window
├── BACS-specific
│   ├── processing_duration_minutes  How long until OPEN→PROCESSING (capped to demo_session_minutes in demo)
│   ├── settlement_duration_minutes  Additional time until PROCESSING→SETTLED (capped to demo_session_minutes in demo)
│   └── input_cutoff                 Display only
└── CHAPS-specific
    └── opening_time / interbank_cutoff
```

| Service | Tick Interval | What it does each tick | Duration source |
|---|---|---|---|
| FPS | `min(demo, 60)s` | Execute forward-dated, standing orders; close expired DNS cycles and reopen new ones | `settlement_times` (production) or `demo_session_minutes` (demo) |
| BACS | `min(demo, 60)s` | Advance OPEN→PROCESSING→AWAITING_SETTLEMENT→SETTLED based on `created_at + config duration` | `processing_duration_minutes` / `settlement_duration_minutes` (capped to demo in demo) |
| CHAPS | `min(demo, 60)s` | Call `EnforceRealtimeLiquidityBlocks` to suspend 2h+ breaches | N/A |

For BACS specifically: `AdvanceCycles` compares cycle `created_at + duration` against `NOW()`, not static `DATE` columns — so sub-day durations work in demo mode. The new cycle created by `CloseInputDay` sets its dates using config durations.

Operating hours are enforced at the handler level:
- **FPS**: `ProcessPayment` and `handleISO8583TCP` reject payments outside `opening_time`-`closing_time` with HTTP 503 or ISO 8583 DE39=91
- **CHAPS**: `ProcessPayment` rejects payments before `opening_time` or after `interbank_cutoff` with HTTP 503
- **BACS**: `handleSubmit` rejects file submissions after `input_cutoff` with HTTP 503

#### Mode switching

Set `"mode": "demo"` or `"mode": "production"` per service in `sessions.json`:

```json
"fps": {
    "mode": "production",
    "demo_session_minutes": 0,
    "settlement_times": ["03:00", "09:00", "15:00"],
    ...
}
```

- **demo**: tick interval = `min(demo_session_minutes, 60)s`, cycle durations capped to `demo_session_minutes`, DNS cycles close every `demo_session_minutes`
- **production**: tick interval = 60s, uses real config durations (1440 min for BACS cycles), uses wall-clock `settlement_times` for FPS DNS

#### Config field status

| Field | Service | Status |
|---|---|---|
| `mode` | all | ✅ Drives tick interval & duration scaling |
| `demo_session_minutes` | all | ✅ Tick interval & DNS cycle window |
| `settlement_times` | FPS | ✅ Next-cycle scheduling via `nextSettlementTime()` |
| `processing_duration_minutes` | BACS | ✅ Drives `AdvanceCycles` + `CloseInputDay` |
| `settlement_duration_minutes` | BACS | ✅ Drives `AdvanceCycles` + `CloseInputDay` |
| `opening_time` | FPS, CHAPS | ✅ Gates payment processing via `checkOperatingHours` / `checkCHAPSHours` |
| `closing_time` | FPS | ✅ Gates payment processing via `checkOperatingHours` |
| `interbank_cutoff` | CHAPS | ✅ Gates payment processing via `checkCHAPSHours` |
| `input_cutoff` | BACS | ✅ Gates file submissions via `checkInputCutoff` |

Scheduler starts in `main.go` via `srv.StartScheduler(schedCtx)` and stops cleanly on SIGINT/SIGTERM via context cancellation. All tasks log on failure but never crash the service.

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
8. **Only BACS is CGO-free**: BACS builds with `CGO_ENABLED=0`. CHAPS and FPS both require CGO for libxml2 XSD validation.
9. **ISO 8583 bitmap encoding**: Bits in the primary bitmap are numbered 1-64 (MSB of byte 0 = bit 1). Bits 65-128 use the secondary bitmap, signaled by bit 1 (MSB) of the primary bitmap. The parser reads bitmap as `binary.BigEndian.Uint64` and checks presence via `1 << (64 - bit)` for primary, `1 << (128 - bit)` for secondary.
10. **Standard 18 amount conversion**: All monetary amounts in BACS Standard 18 files are stored as pence (whole integers). The parser divides by 100.0 to produce GBP float values. This applies to Record 1 (TotalValue), Record 3 (Amount), Record 4 (Amount), Record 9 (TotalValue), and Record A (Amount).
11. **FPS content-type dispatch**: `ProcessPayment` handles three content types — `application/json` (direct entry), `application/xml` (ISO 20022 pacs.008), and `application/octet-stream` (ISO 8583 binary 0200 message). Each is routed to a dedicated handler. The ISO 8583 handler converts DE4 from pence to pounds (`amount/100.0`) before settlement.
12. **Sort code support**: BACS (Standard 18 parser), CHAPS (ISO 20022 + JSON API), and FPS (ISO 20022 + JSON API) all support UK bank sort codes. In CHAPS and FPS, sort codes are optional fields on `participant_profiles.sort_code` and `transactions.sender_sort_code`/`receiver_sort_code`. XML pacs.008 messages parse sort codes from `ClrSysMmbId>MmbId` within `FinInstnId`. ISO 8583 does not carry sort codes (empty strings passed). Sort codes are stored as `VARCHAR(9)` — either `XX-XX-XX` or `XXXXXX` format.
13. **Gridlock retry on settlement**: When a payment fails due to insufficient liquidity (PDNG/INSU), `SettlePayment` and `SettleSIP` automatically call `ResolveGridlock()` and retry once before queueing. This ensures incoming queued payments are settled first, potentially freeing up liquidity. Only PDNG triggers the retry — not RJCT or ACTC.
14. **SSE EventBus is in-memory**: The `pkg/events.EventBus` uses a `map[BIC][]chan` with no persistence. Events published before a client connects are lost. Reconnecting clients only receive events published after reconnection. This is by design — SSE is for real-time notifications, not durable event sourcing.
15. **FPS ISO 8583 TCP socket**: FPS listens on `:7421` (configurable via `ISO8583_PORT` env var) for raw TCP connections. Uses 2-byte big-endian length prefix framing, max 4096 bytes per message, goroutine-per-connection. The TCP handler uses the same `Ledger.SettleSIP` and `Events.Publish` calls as the HTTP handler.

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
                     │      PostgreSQL 18 x3          │
                     │  (separate databases:          │
                     │   chaps_ledger, fps_ledger,    │
                     │   bacs_ledger)                 │
                     └────────────────────────────────┘
```

Each service is an independent Go binary + optional React GUI, deployable together via Docker Compose. Each service runs its own PostgreSQL database with its own participant tables, transaction tables, and audit trail — no shared infrastructure. All three follow the same normalized schema pattern (participants split across 3 tables).
