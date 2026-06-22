# UKPS Codebase Guide for AI Agents

## Repository Overview

```
ukps/                          # Root — UK Payment Systems (uni project)
├── AGENTS.md                  # ← This file
├── .gitignore
├── bacs-service/              # IMPLEMENTED — BACS (Standard 18), batch settlement, Go backend
├── fps-service/               # IMPLEMENTED — FPS (ISO 20022 + ISO 8583), near-real-time, Go backend
├── chaps-service/             # IMPLEMENTED — CHAPS (ISO 20022), RTGS, Go backend
```

Three services mimic the UK interbank payment network. All three services have fully implemented Go backends with real business logic.

---

## Service Port Map

| Service | Port | DB Port | DB Name | Other |
| :--- | :--- | :--- | :--- | :--- |
| CHAPS | 8080 | 5432 | `chaps_ledger` | — |
| FPS | 8081 | 5433 | `fps_ledger` | TCP `7421` (ISO 8583 socket) |
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
- `participant_profiles` — static BIC/name/currency/sort_code (UK bank sort code, required)
- `participant_liquidity` — high-frequency balance updates
- `participant_statuses` — ACTIVE/SUSPENDED/DISABLED + block info
- `transactions` — payment records, UUID v7 primary key, includes sender/receiver sort codes (required in JSON/XML, empty in ISO 8583)
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
7. Create `Dockerfile`, `compose.yml`
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

## API Reference

### Conventions

| Aspect | Convention |
|---|---|
| Base path | `/v1/{resource}/{scheme}[/{id}[/{action}]]` |
| Auth | `Authorization: Bearer <api_key>` (BIC derived from API key via DB lookup) |
| Unauthenticated | `GET /v1/healthz`, `GET /v1/system/schedule`, `POST /v1/participants/register`, participant list/get/block* by BIC, payment list/get/validate |
| Content-Type | `application/json` (GUI), `application/xml` (ISO 20022 pacs.008 returns pacs.002), `application/octet-stream` (ISO 8583 binary for FPS) |
| Idempotency | `Idempotency-Key` header accepted as alternative to body `msg_id` |
| SSE | `GET /v1/payments/{scheme}/incoming` — real-time events, BIC from auth header |
| Errors | `{"error": "message"}` |
| Status codes | 200 (success), 201 (created), 202 (accepted/queued), 400 (bad request), 401 (unauthorized), 404 (not found), 409 (conflict), 415 (unsupported media), 500 (internal), 503 (unavailable — operating hours) |

### Common Error Responses

| Code | Body |
|---|---|
| 400 | `{"error": "msg_id, receiver_bic, and positive amount are required"}` |
| 401 | `{"error": "invalid API key"}` |
| 404 | `{"error": "Payment not found"}` |
| 409 | `{"error": "This payment cannot be cancelled"}` |
| 415 | `{"error": "Unsupported Media Type"}` |
| 503 | `{"error": "Service is currently unavailable (outside operating hours)"}` |

### Payment Status Values

| Internal Status | ISO 20022 Status | Meaning |
|---|---|---|
| `SETTLED` | `ACTC` | Accepted — technically settled |
| `QUEUED` / `PENDING` | `PDNG` | Pending — queued for later settlement |
| `REJECTED` | `RJCT` | Rejected — not processed |

### SSE Events

| Scheme | Endpoint | Event Type | Published On |
|---|---|---|---|
| CHAPS | `GET /v1/payments/chaps/incoming` | `payment.received` | Each ACTC settlement |
| FPS | `GET /v1/payments/fps/incoming` | `payment.received` | Each ACTC settlement |
| BACS | `GET /v1/payments/bacs/incoming` | `cycle.settled` | Cycle `SettleCycle` call |

SSE payload format:
```
data: {"type":"payment.received","data":{"msg_id":"CHAPS-...","sender":"BARCGB22","receiver":"MIDLGB22","receiver_sort_code":"40-05-15","receiver_account":"12345678","amount":1500000,"status":"SETTLED","scheme":"CHAPS"}}
```

---

### Shared Endpoints (All Three Services)

#### POST /v1/participants/register

Register a new participant. Unauthenticated. Returns an API key.

| Field | Type | Required | Notes |
|---|---|---|---|
| `bic` | string | yes | 8-11 alphanumeric |
| `name` | string | yes | Bank name |
| `sort_code` | string | yes | `XX-XX-XX` or `XXXXXX` |
| `balance` | float | no | Initial balance (default 0) |

FPS-only extra: `participant_type` (DIRECT/INDIRECT), `sponsor_bic`
BACS-only extra: `su_code`, `is_service_user`, `is_destination_user`

Response `201`: `{"bic": "...", "api_key": "apk_...", "status": "ACTIVE"}`

#### PATCH /v1/participants/{bic}

Update participant details. Unauthenticated.

Body: `{"name": "...", "sort_code": "...", "balance": 1000000}` (+ FPS/BACS-specific fields)

Response `200`: `{"bic": "...", "status": "updated"}`

#### DELETE /v1/participants/{bic}

Delete a participant. Unauthenticated.

Response `200`: `{"bic": "...", "status": "deleted"}`
Response `409`: if participant has pending transactions

#### GET /v1/participants

List all participants. Unauthenticated.

Response `200`:
```json
[{"bic": "BARCGB22", "name": "Barclays Bank", "sort_code": "20-00-00", "status": "ACTIVE",
  "balance": 50000000, "currency": "GBP", "is_closed": false,
  "overdraft_limit": 10000000, "block_reason": null}]
```

#### GET /v1/participants/positions

Get own liquidity position. Authenticated. BIC from API key.

Response `200`: `{"bic": "BARCGB22", "balance": 50000000, "earmarked": 1500000, "available": 48500000}`

#### PATCH /v1/participants/{bic}/status and PATCH /v1/participants/status

Update participant status. The `{bic}` variant is unauthenticated; the `/status` variant uses auth.

Body: `{"status": "SUSPENDED", "reason": "FRAUD_SUSPECTED"}` — status must be ACTIVE, SUSPENDED, or DISABLED.

Response `200`: `{"bic": "...", "status": "SUSPENDED"}`

#### POST /v1/participants/{bic}/block and POST /v1/participants/block

Block a participant (sets SUSPENDED). `{bic}` variant is unauthenticated; `/block` uses auth.

Body: `{"reason": "FRAUD_SUSPECTED"}` (optional, defaults to `FRAUD_SUSPECTED`)

Response `200`: `{"bic": "...", "status": "SUSPENDED", "reason": "FRAUD_SUSPECTED"}`

#### GET /v1/participants/{bic}/block and GET /v1/participants/block

Get block details. `{bic}` variant unauthenticated; `/block` uses auth.

Response `200`: `{"bic": "...", "reason": "FRAUD_SUSPECTED", "blocked_at": "..."}`
Response `404`: if not blocked

#### DELETE /v1/participants/{bic}/block and DELETE /v1/participants/block

Unblock a participant (sets ACTIVE). `{bic}` variant unauthenticated; `/block` uses auth.

Response `200`: `{"bic": "...", "status": "ACTIVE"}`

#### POST /v1/liquidity/top-up

Add funds to own balance. Authenticated.

Body: `{"amount": 5000000.00}`

Response `200`: `{"bic": "...", "status": "UPDATED"}`

#### GET /v1/healthz

Health check. Unauthenticated.

Response `200`: `{"status": "ok", "service": "CHAPS"}` (service varies per service)

#### GET /v1/system/schedule

Operating hours and demo config. Unauthenticated.

Response `200` (CHAPS): `{"date": "2026-06-22", "opening_time": "07:00", "interbank_cutoff": "18:00", "timezone": "Europe/London", "demo_session_minutes": "0"}`
Response `200` (FPS): includes `closing_time`, `settlement_times` array
Response `200` (BACS): includes `input_cutoff`, `settlement: "T+2"`

---

### CHAPS Endpoints (port 8080)

#### POST /v1/payments/chaps

Submit a CHAPS payment. Authenticated. Content-Type dispatch:
- `application/json` → direct JSON entry (see below)
- `application/xml` or `text/xml` → ISO 20022 pacs.008 with XSD validation

JSON Mode Body:
| Field | Type | Required | Notes |
|---|---|---|---|
| `msg_id` | string | yes | Unique (idempotency), max 35 chars |
| `end_to_end_id` | string | no | End-to-end reference |
| `receiver_bic` | string | yes | Target BIC |
| `receiver_sort_code` | string | yes | `XX-XX-XX` or `XXXXXX` |
| `receiver_account` | string | yes | Account number |
| `amount` | float | yes | GBP amount (> 0) |

Sender BIC is derived from auth header (the authenticated participant is always the sender).

XML Mode: BizMsg envelope containing pacs.008, validated against `chaps_wrapper.xsd`. Sender/receiver BICs, sort codes parsed from `FinInstnId/BICFI` and `ClrSysMmbId/MmbId` in `DbtrAgt`/`CdtrAgt`.

Response `200` (JSON): `{"msg_id": "...", "status": "SETTLED", "iso_status": "ACTC", "reason_code": ""}`
Response `200` (XML): BizMsg envelope containing pacs.002 with ACTC
Response `202` (JSON): `{"msg_id": "...", "status": "QUEUED", "iso_status": "PDNG", "reason_code": "INSU"}` (insufficient liquidity)
Response `202` (XML): pacs.002 with PDNG or RJCT

Header `X-Transaction-Status` set on all responses.

#### POST /v1/payments/chaps/validate

Pre-flight validation without settling. Unauthenticated.

Body: `{"sender_bic": "BARCGB22", "receiver_bic": "MIDLGB22", "amount": 1500000}`

Response `200`: `{"valid": true, "checks": ["bic_format", "positive_amount", "limit_check", "participant_exists", "sender_active", "sufficient_liquidity"], "errors": [], "available": 5000000}`

#### GET /v1/payments/chaps

List CHAPS payments. Unauthenticated. Query params: `?status=SETTLED&limit=50`

Response `200`:
```json
[{"msg_id": "CHAPS-...", "sender_bic": "BARCGB22", "receiver_bic": "MIDLGB22",
  "sender_sort_code": "20-00-00", "receiver_sort_code": "40-05-15",
  "amount": 1500000, "status": "SETTLED", "created_at": "2026-06-22T10:00:00Z"}]
```

#### GET /v1/payments/chaps/{id}

Get single payment details with audit trail. Unauthenticated.

Response `200`:
```json
{"msg_id": "...", "status": "SETTLED", "amount": 1500000, "sender_bic": "BARCGB22",
 "receiver_bic": "MIDLGB22", "sender_sort_code": "20-00-00", "receiver_sort_code": "40-05-15",
 "end_to_end_id": "E2E-REF-001", "created_at": "...",
 "audit_trail": [{"bic": "BARCGB22", "amount": -1500000}, {"bic": "MIDLGB22", "amount": 1500000}]}
```

#### DELETE /v1/payments/chaps/{id}

Cancel a pending/queued CHAPS payment. Releases earmarked funds.

Response `200`: `{"msg_id": "...", "status": "CANCELLED"}`
Response `409`: if payment is already SETTLED

#### POST /v1/payments/chaps/{id}/authorize

Authorize a pending/queued payment for settlement. Unauthenticated.

Response `200`: `{"msg_id": "...", "status": "SETTLED"}`

#### POST /v1/payments/chaps/{id}/amend

Amend a pending payment's end-to-end ID. Unauthenticated.

Body: `{"end_to_end_id": "NEW-REF-001"}`

Response `200`: `{"msg_id": "...", "status": "AMENDED"}`

#### GET /v1/payments/chaps/limits

Get CHAPS clearing limits. Unauthenticated. Query: `?bic=BARCGB22`

Response `200`:
```json
{"currency": "GBP", "single_payment_limit": 20000000, "daily_participant_limit": 10000000,
 "total_available_liquidity": 500000000, "remaining_intraday_liquidity": 150000000}
```

#### PATCH /v1/payments/chaps/limits

Update own overdraft limit. Authenticated.

Body: `{"overdraft_limit": 1000000.00}`

Response `200`: `{"bic": "...", "overdraft_limit": 1000000}`

#### POST /v1/payments/chaps/gridlock/resolve

Resolve payment gridlock — settles queued payments that deadlock on liquidity. Unauthenticated.

Response `200`: `{"status": "COMPLETED", "settled": 3}`

---

### FPS Endpoints (port 8081)

#### POST /v1/payments/fps

Submit an FPS payment. Authenticated. Three content-type modes:
- `application/json` → SIP settlement (same body shape as CHAPS JSON)
- `application/xml` → ISO 20022 pacs.008 with XSD validation
- `application/octet-stream` → ISO 8583 binary 0200 message

JSON Body: same as CHAPS: `{"msg_id", "end_to_end_id", "receiver_bic", "receiver_sort_code", "receiver_account", "amount"}`

ISO 8583 Binary (0200):
| Field | DE | Format | Notes |
|---|---|---|---|
| MTI | — | 4 ASCII chars | Always `0200` |
| DE2 PAN | 2 | LLVAR ASCII | Primary account number |
| DE3 ProcCode | 3 | 6 ASCII | Processing code |
| DE4 Amount | 4 | 12 ASCII | In **pence** (e.g. `0000002500000` = £250,000.00) |
| DE7 TransDateTime | 7 | 10 ASCII | MMDDHHMMSS |
| DE11 Trace | 11 | 6 ASCII | Trace number |
| DE32 Acquirer | 32 | LLVAR ASCII | Sender BIC |
| DE37 RefNum | 37 | 12 ASCII | Reference number |
| DE41 TerminalID | 41 | 8 ASCII | Terminal ID |
| DE49 Currency | 49 | 3 ASCII | `826` for GBP |
| DE100 Receiver | 100 | LLVAR ASCII | Receiver BIC |
| DE102 Source Account | 102 | LLVAR ASCII | Source account |
| DE103 Dest Account | 103 | LLVAR ASCII | Destination account |

Response `200` (JSON): `{"msg_id": "...", "status": "SETTLED", "iso_status": "ACTC", "reason_code": ""}`
Response `200` (ISO 8583): Binary 0210 message with DE39=`00` (approved)
Response `202` (ISO 8583): Binary 0210 with DE39=`51` (PDNG) or `57` (RJCT)

#### GET /v1/payments/fps

List FPS payments. Unauthenticated. Query: `?status=SETTLED&limit=50`

Response includes `payment_type` field (SIP, DNS, FORWARD_DATED, STANDING_ORDER).

#### POST /v1/payments/fps/validate

Pre-flight validation. Unauthenticated.

Body: `{"sender_bic": "BARCGB22", "receiver_bic": "MIDLGB22", "amount": 250000}`

Response `200`: `{"valid": true, "sender_bic": "...", "receiver_bic": "...", "amount": 250000, "reason": "", "checks_passed": [...]}`

#### GET /v1/payments/fps/{id}

Get FPS payment details. Unauthenticated.

Response `200`: includes same fields as CHAPS payment details.

#### DELETE /v1/payments/fps/{id}

Cancel/recall an FPS payment. Only SIP payments can be cancelled.

Response `200`: `{"msg_id": "...", "status": "CANCELLED"}`

#### POST /v1/payments/fps/forward-dated

Schedule a forward-dated payment. Authenticated.

Body: `{"msg_id": "...", "receiver_bic": "MIDLGB22", "amount": 250000, "execution_date": "2026-06-25"}`

Response `201`: `{"msg_id": "...", "status": "SCHEDULED"}`

#### GET /v1/payments/fps/forward-dated

List forward-dated payments. Unauthenticated.

#### DELETE /v1/payments/fps/forward-dated/{id}

Cancel a forward-dated payment. Unauthenticated.

#### POST /v1/payments/fps/standing-orders

Create a standing order. Authenticated.

Body: `{"reference": "SO-001", "receiver_bic": "MIDLGB22", "amount": 5000, "frequency": "MONTHLY", "next_date": "2026-07-01", "end_date": "2027-07-01"}`

Frequency values: DAILY, WEEKLY, MONTHLY, YEARLY

Response `201`: `{"reference": "...", "status": "ACTIVE"}`

#### GET /v1/payments/fps/standing-orders

List standing orders. Unauthenticated.

#### GET /v1/payments/fps/standing-orders/{id}

Get single standing order details. Unauthenticated.

#### PATCH /v1/payments/fps/standing-orders/{id}

Update a standing order. Unauthenticated.

Body: `{"frequency": "WEEKLY", "amount": 10000, "next_date": "2026-08-01", "end_date": "2027-08-01"}`

Response `200`: `{"id": "...", "status": "UPDATED"}`

#### DELETE /v1/payments/fps/standing-orders/{id}

Cancel a standing order. Unauthenticated.

Response `200`: `{"id": "...", "status": "CANCELLED"}`

#### POST /v1/payments/fps/bulk

Create a bulk submission. Authenticated.

Body: `{"filename": "bulk_001.csv", "total_items": 100, "total_value": 500000}`

Response `201`: `{"id": "...", "status": "RECEIVED"}`

#### GET /v1/payments/fps/bulk/{id}

Get bulk submission details. Unauthenticated.

#### POST /v1/payments/fps/iso8583

Submit ISO 8583 binary 0200 message over HTTP. Authenticated. Same field structure as the ISO 8583 handler in `POST /v1/payments/fps` with `Content-Type: application/octet-stream`.

Response `200`: Binary 0210 with DE39=`00`
Response `202`: Binary 0210 with DE39=`51` or `57`

#### GET /v1/payments/fps/iso8583/decode

Decode an ISO 8583 binary message without settling. Unauthenticated. Sends binary body, receives JSON with parsed fields.

#### GET /v1/payments/fps/limits

Get FPS limits. Unauthenticated. Query: `?bic=BARCGB22`

Response same shape as CHAPS limits.

#### PATCH /v1/payments/fps/limits

Update own overdraft limit. Authenticated.

Body: `{"overdraft_limit": 2000000}`

#### POST /v1/payments/fps/gridlock/resolve

Resolve FPS gridlock (queued SIP payments). Unauthenticated.

#### FPS DNS Settlement

| Endpoint | Method | Description |
|---|---|---|
| `/v1/settlement/dns/cycle` | GET | Get current DNS cycle |
| `/v1/settlement/dns/close` | POST | Close DNS cycle — nets QUEUED transactions, settles net positions. Response: `{"status": "CLOSED", "net_positions": [{"bic": "...", "net_position": ...}]}` |
| `/v1/settlement/dns/history` | GET | DNS cycle history |

#### GET /v1/liquidity/prefunded

Get own prefunded balance. Authenticated.

Response `200`: `{"bic": "...", "prefunded_balance": 10000000}`

---

### BACS Endpoints (port 8082)

#### POST /v1/payments/bacs/submit

Submit a BACS Standard 18 file. Authenticated. Accepts:
- `multipart/form-data` with file field `"file"`
- Raw body with `Content-Type: text/plain` (use `?filename=` query param)

Record types parsed:
| Record | Type | Content |
|---|---|---|
| 1 | Header | Volume no, destination sort code/account, total value (pence ÷ 100), total volume, date |
| 2 | Volume header | Originator/ destination, processing date, value, SU code, reference |
| 3 | Direct Debit | Destination sort code/account, amount (pence ÷ 100), originator, reference |
| 4 | Direct Credit | Destination sort code/account, amount (pence ÷ 100), originator name, reference |
| 5 | Trailer (DD) | Volume no, record count |
| 9 | Trailer (DC) | Volume no, total value (pence ÷ 100), total volume, hash total |
| A | AUDDIS | Instruction code, sort code, account, reference, amount (pence ÷ 100) |

Response `201`:
```json
{"id": 1, "filename": "payments.txt", "volume": 100, "value": 500000.00,
 "status": "RECEIVED", "cycle_id": 1, "su_bic": "BARCGB22"}
```

Response `202` on partial store: `{"id": 1, "status": "RECEIVED", "error": "transactions may be partially stored"}`
Response `503` if outside input cutoff or no open cycle.

#### GET /v1/payments/bacs/submit/{id}

Get submission details. Unauthenticated.

#### GET /v1/payments/bacs/submit

List submissions. Unauthenticated. Query: `?status=RECEIVED&su_bic=BARCGB22`

#### DELETE /v1/payments/bacs/submit/{id}

Recall a submission (only if cycle is still OPEN). Unauthenticated.

Response `200`: `{"id": ..., "status": "RECALLED"}`

#### BACS Cycle Management

| Endpoint | Method | Description | Transition |
|---|---|---|---|
| `/v1/payments/bacs/cycle/current` | GET | Get current cycle | — |
| `/v1/payments/bacs/cycle/{cycleDate}` | GET | Get cycle by date (YYYY-MM-DD) | — |
| `/v1/payments/bacs/cycle` | GET | List all cycles | — |
| `/v1/payments/bacs/cycle/close` | POST | Close input day | OPEN → PROCESSING |
| `/v1/payments/bacs/cycle/process` | POST | Process cycle | PROCESSING → AWAITING_SETTLEMENT |
| `/v1/payments/bacs/cycle/settle` | POST | Settle cycle | AWAITING_SETTLEMENT → SETTLED |

Cycle state machine: `OPEN → PROCESSING → AWAITING_SETTLEMENT → SETTLED`

#### POST /v1/payments/bacs/mandates

Create a BACS Direct Debit mandate (AUDDIS). Unauthenticated.

Body: `{"reference": "MAND-001", "su_bic": "BARCGB22", "payer_name": "John Smith", "payer_sort_code": "40-05-15", "payer_account": "12345678", "amount": 500.00, "frequency": "MONTHLY"}`

Response `201`: `{"id": 1, "reference": "MAND-001", "status": "ACTIVE"}`

#### GET /v1/payments/bacs/mandates

List mandates. Unauthenticated. Query: `?su_bic=BARCGB22`

#### GET /v1/payments/bacs/mandates/{ref}

Get mandate details. Unauthenticated.

#### PATCH /v1/payments/bacs/mandates/{ref}

Amend mandate. Unauthenticated.

Body: `{"amount": 750.00, "frequency": "WEEKLY"}`

Response `200`: `{"reference": "MAND-001", "status": "AMENDED"}`

#### DELETE /v1/payments/bacs/mandates/{ref}

Cancel mandate. Unauthenticated.

Response `200`: `{"reference": "MAND-001", "status": "CANCELLED"}`

#### POST /v1/payments/bacs/mandates/{ref}/claim

Claim a mandate — link payer account. Unauthenticated.

Body: `{"payer_sort_code": "40-05-15", "payer_account": "12345678"}`

Response `200`: `{"reference": "MAND-001", "status": "CLAIMED"}`

#### BACS Returns & Rejects

| Endpoint | Method | Description |
|---|---|---|
| `/v1/payments/bacs/returns` | GET | List ARUCS returns |
| `/v1/payments/bacs/returns` | POST | Create return. Body: `{"original_transaction_id": 1, "reason_code": "AC-01", "amount": 500}` |
| `/v1/payments/bacs/rejects` | GET | List rejected submissions |

#### BACS Reports

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/v1/payments/bacs/reports/{cycleDate}` | GET | No | Cycle reports — list submissions with volume/value |
| `/v1/payments/bacs/reports/{cycleDate}/su` | GET | Yes | SU-specific cycle reports (BIC from auth) |
| `/v1/payments/bacs/reports/{cycleDate}/summary` | GET | No | Cycle summary: total submissions, volume, value |
| `/v1/payments/bacs/reports/{cycleDate}/netting` | GET | No | Netting report (query: `?bic=`). Returns bilateral net positions |
| `/v1/payments/bacs/su/reports` | GET | Yes | SU reports (BIC from auth) |

`GET .../netting` response:
```json
{"cycle_id": 1, "cycle_date": "2026-06-22", "cycle_status": "SETTLED",
 "bilateral": [{"debtor_bic": "...", "creditor_bic": "...", "gross_amount": ..., "net_amount": ...}],
 "net_positions": [{"bic": "...", "net_position": ..., "balance_before": ..., "overdraft_limit": ..., "status": "..."}]}
```

#### GET /v1/payments/bacs/limits

Get BACS system limits. Unauthenticated.

Response `200`:
```json
{"max_file_size": 1000000, "max_transactions_per_file": 100000, "max_submission_value": 50000000.00,
 "total_system_liquidity": ..., "settlement_cycle": "T+2", "currency": "GBP"}
```

#### PATCH /v1/payments/bacs/limits

Update own overdraft limit. Authenticated.

Body: `{"overdraft_limit": 5000000}`

Response `200`: `{"bic": "...", "status": "LIMITS_UPDATED", "overdraft_limit": 5000000}`

---

### KLIK Endpoints (CHAPS integration, port 8080)

| Endpoint | Method | Description |
|---|---|---|
| `/v1/klik/chaps/settle` | POST | Settle via bank name → BIC resolution. Body: `{"session_id", "transfer_id", "from": "Barclays Bank", "to": "HSBC", "amount": "250.00", "currency": "GBP"}`. Response: `{"transfer_id": "...", "status": "SUCCESS", "rtgs_reference": "CHAPS-TXN-001"}` |
| `/v1/klik/chaps/healthz` | GET | Returns `{"status": "ok", "system": "CHAPS"}` |

---

### ISO 8583 TCP Socket (FPS only)

- Port: `7421` (configurable via `ISO8583_PORT` env var)
- Framing: 2-byte big-endian length prefix, then raw ISO 8583 binary
- Max message size: 4096 bytes
- Goroutine-per-connection model
- Handles 0200 → responds with 0210 (same field structure as HTTP ISO 8583 handler)
- Uses same `Ledger.SettleSIP` and gridlock retry as HTTP

---

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
- `github.com/lestrrat-go/libxml2` + XSD validation for ISO 20022 (CHAPS and FPS)
- Package name matches directory name
- Error sentinel values: `var ErrX = errors.New("...")`
- `pkg/events/events.go` — in-memory EventBus for SSE real-time notifications

### Docker conventions
- Multi-stage build: `golang:1.26-alpine` → `alpine:3.23`
- Static link libxml2 with CGO (CHAPS and FPS only; BACS is CGO-free)
- Port 8080 (CHAPS), 8081 (FPS), 8082 (BACS)
- DB runs in separate container (Postgres 18-alpine)
- `compose.yml` for production (DB + app)

### Testing
- Test files exist for ISO 8583 parser, ISO 20022 serialization, and Standard 18 parser:
  - `chaps-service/pkg/iso20022/serialization_test.go` — 17 tests (includes pacs.008 with sort code XML parsing, without sort codes)
  - `chaps-service/pkg/ledger/service_test.go` — 12 tests (includes sort code struct fields)
  - `chaps-service/pkg/server/server_test.go` — 14 tests (includes JSON sort code and register sort code tests)
  - `fps-service/pkg/iso8583/message_test.go` — 7 tests (short msg, MTI validation, optional fields, amount+trace, 0210 encode, round-trip, full field parse)
  - `fps-service/pkg/iso20022/serialization_test.go` — 14 tests (includes pacs.008 sort code XML parsing)
  - `fps-service/pkg/server/server_test.go` — 4 tests (JSON sort code and register sort code tests)
  - `bacs-service/pkg/standard18/parser_test.go` — 13 tests (basic file, AUDDIS, CRLF, validation, pence conversion, multiple records, line padding, zero values)
- Integration smoke test: `test/integration_test.sh` — runs HTTP smoke tests (participants, cycles, etc.)
- When adding tests:
  - Go: `_test.go` files alongside source with `package X_test`
  - SQL: use Docker compose-dev + manual seed verification

### SSE real-time events

All three services support server-sent events for real-time payment notifications.

| Service | Endpoint | Published On | Event Type |
|---|---|---|---|---|
| CHAPS | `GET /v1/payments/chaps/incoming` | Each `ACTC` settlement | `payment.received` |
| FPS | `GET /v1/payments/fps/incoming` | Each `ACTC` settlement | `payment.received` |
| BACS | `GET /v1/payments/bacs/incoming` | Cycle `SettleCycle` call | `cycle.settled` |

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
│   └── opening/closing_time  Display only (not enforced)
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
2. **CGO is required for CHAPS and FPS**: libxml2 bindings for XSD validation use CGO. Build with musl tags for Alpine. BACS is CGO-free.
3. **Database URL forces TCP**: Default `DATABASE_URL` uses `127.0.0.1` instead of `localhost` to avoid Unix socket ambiguity.
4. **Postgres 18 specific**: uses `uuidv7()` function not present in older versions.
5. **Auth via API key**: All authenticated endpoints require `Authorization: Bearer <api_key>`. The BIC is derived from the API key via a DB lookup in the auth middleware. `GET /v1/healthz` is the only unauthenticated endpoint. The `POST /v1/participants/register` endpoint returns the generated `api_key` in the response body.
6. **`xsd/chaps_wrapper.xsd`** is a *custom* envelope — not standard ISO 20022. It wraps `AppHdr` + `Document` for single-XSD validation.
7. **pacs.009 and pacs.029 XSDs** are included but unused — available for extension (bank-to-bank transfers, investigation messages).
8. **FPS and CHAPS require CGO**: Both use libxml2 for XSD validation (via `pkg/validator/`). BACS builds with `CGO_ENABLED=0` (no XSD validation needed — Standard 18 uses Go's native parser).
9. **ISO 8583 bitmap encoding**: Bits in the primary bitmap are numbered 1-64 (MSB of byte 0 = bit 1). Bits 65-128 use the secondary bitmap, signaled by bit 1 (MSB) of the primary bitmap. The parser reads bitmap as `binary.BigEndian.Uint64` and checks presence via `1 << (64 - bit)` for primary, `1 << (128 - bit)` for secondary.
10. **Standard 18 amount conversion**: All monetary amounts in BACS Standard 18 files are stored as pence (whole integers). The parser divides by 100.0 to produce GBP float values. This applies to Record 1 (TotalValue), Record 3 (Amount), Record 4 (Amount), Record 9 (TotalValue), and Record A (Amount).
11. **FPS content-type dispatch**: `ProcessPayment` handles three content types — `application/json` (direct entry), `application/xml` (ISO 20022 pacs.008), and `application/octet-stream` (ISO 8583 binary 0200 message). Each is routed to a dedicated handler. The ISO 8583 handler converts DE4 from pence to pounds (`amount/100.0`) before settlement.
12. **Sort code support**: BACS (Standard 18 parser), CHAPS (ISO 20022 + JSON API), and FPS (ISO 20022 + JSON API) all require UK bank sort codes for participant registration (`participant_profiles.sort_code` is `NOT NULL`). Transaction-level sort codes (`sender_sort_code`/`receiver_sort_code`) are required for JSON and XML payments; ISO 8583 does not carry sort codes (empty strings passed). XML pacs.008 messages parse sort codes from `ClrSysMmbId>MmbId` within `FinInstnId`. Sort codes are stored as `VARCHAR(9)` — either `XX-XX-XX` or `XXXXXX` format.
13. **Gridlock retry on settlement**: When a payment fails due to insufficient liquidity (PDNG/INSU), `SettlePayment` and `SettleSIP` automatically call `ResolveGridlock()` and retry once before queueing. This ensures incoming queued payments are settled first, potentially freeing up liquidity. Only PDNG triggers the retry — not RJCT or ACTC.
14. **SSE EventBus is in-memory**: The `pkg/events.EventBus` uses a `map[BIC][]chan` with no persistence. Events published before a client connects are lost. Reconnecting clients only receive events published after reconnection. This is by design — SSE is for real-time notifications, not durable event sourcing.
15. **FPS ISO 8583 TCP socket**: FPS listens on `:7421` (configurable via `ISO8583_PORT` env var) for raw TCP connections. Uses 2-byte big-endian length prefix framing, max 4096 bytes per message, goroutine-per-connection. The TCP handler uses the same `Ledger.SettleSIP` and `Events.Publish` calls as the HTTP handler.

---

## How This Fits Together: UKPS Architecture

```
┌────────────────────┐    ┌─────────────────────┐    ┌────────────────────┐
│   bacs-service     │    │    fps-service      │    │   chaps-service    │
│   (Standard 18)    │    │ (ISO20022+ISO8583)  │    │   (ISO 20022)      │
│   Batch / 3-day    │    │   Near-real-time    │    │   RTGS / High-val  │
│   Port 8082        │    │   Port 8081         │    │   Port 8080        │
└────────┬───────────┘    └────────┬────────────┘    └────────┬───────────┘
         │                         │                         │
         ▼                         ▼                         ▼
┌──────────────────┐    ┌──────────────────┐    ┌──────────────────┐
│  PostgreSQL 18   │    │  PostgreSQL 18   │    │  PostgreSQL 18   │
│  bacs_ledger     │    │  fps_ledger      │    │  chaps_ledger    │
│  Port 5434       │    │  Port 5433       │    │  Port 5432       │
│  • participants  │    │  • participants  │    │  • participants  │
│  • cycles        │    │  • dns_cycles    │    │  • transactions  │
│  • submissions   │    │  • transactions  │    │  • journal       │
│  • transactions  │    │  • standing_ord  │    │                  │
│  • net positions │    │  • forward_dated │    │                  │
│  • journal       │    │  • journal       │    │                  │
└──────────────────┘    └──────────────────┘    └──────────────────┘
```

Each service is an independent Go binary, deployable together via Docker Compose. They communicate with external systems via HTTP (all three), ISO 8583 TCP socket (FPS), or file upload (BACS). There is **no direct inter-service database access** — each service has its own isolated PostgreSQL instance with its own participant registry.

Each service maintains its own `participant_profiles` table with scheme-specific columns:

| Column | CHAPS | FPS | BACS |
|---|---|---|---|
| `sort_code` | ✅ optional | ✅ optional | ❌ |
| `participant_type` (DIRECT/INDIRECT) | ❌ | ✅ | ❌ |
| `sponsor_bic` | ❌ | ✅ | ❌ |
| `su_code` | ❌ | ❌ | ✅ |
| `is_service_user` | ❌ | ❌ | ✅ |
| `is_destination_user` | ❌ | ❌ | ✅ |

The same 4 banks are seeded in all three services but with scheme-specific balances and attributes.

A hypothetical central bank service could make HTTP REST calls (`POST /v1/liquidity/top-up`) to manage liquidity across schemes.
