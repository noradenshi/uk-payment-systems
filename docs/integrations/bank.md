# Bank Integration Guide — UK Payment Systems (UKPS)

## Overview

UKPS is a multi-scheme interbank payment system simulator. Three independent
services run in parallel, each modelling a real UK payment scheme:

| Service | Scheme | Settlement | Target Value | Port |
| :--- | :--- | :--- | :--- | :--- |
| CHAPS | Real-Time Gross Settlement (RTGS) | Immediate, irrevocable per-payment | High-value (£1M+) | 8080 |
| FPS | Faster Payments | Near-real-time (SIP) + Deferred Net Settlement (DNS) | Low-value (<£250K) | 8081 |
| BACS | BACS | Batch, 3-day cycle (input → processing → settlement) | Low-value, high-volume | 8082 |

Each service runs its own PostgreSQL instance with an independent participant
registry. There is no shared database or direct inter-service communication.

---

## Participating Banks (seed)

Four banks are seeded in all three services. Each service stores its own copy
with scheme-specific balances and attributes.

### Registered Participants

| Bank | BIC | Sort Code |
| :--- | :--- | :--- |
| Barclays Bank | `BARCGB2L` | `20-00-00` |
| HSBC UK | `HSBCGB44` | `40-00-00` |
| Lloyds Bank | `LLOYGB21` | `30-00-00` |
| Alice Bank | `SNDRUK22` | `60-00-00` |

### Balances per Scheme

| Bank | CHAPS | FPS | BACS |
| :--- | :--- | :--- | :--- |
| Barclays | £1,000,000 | £500,000 | £1,000,000 |
| HSBC | £500,000 | £300,000 | £800,000 |
| Lloyds | £750,000 | £400,000 | £750,000 |
| Alice Bank | £1,000,000 | £500,000 | £500,000 |

### Authentication

All API endpoints (except `GET /v1/healthz`) require an API key sent via the
`Authorization` header:

```
Authorization: Bearer <api_key>
```

The API key identifies the calling bank (BIC). There is no `{bic}` in path
parameters — the BIC is derived from the API key server-side.

API keys are generated automatically during registration. The 4 seed banks
have pre-assigned keys (see Seed API Keys below).

### Onboarding a New Bank

```
POST /v1/participants/register
Content-Type: application/json

{
  "bic": "ABCDGB2L",
  "name": "New Bank",
  "sort_code": "12-34-56",
  "balance": 500000.00
}
```

Response:

```json
{
  "bic": "ABCDGB2L",
  "api_key": "ukps_3a1f2b8c4d7e9f0a1b2c3d4e5f6a7b8c",
  "status": "ACTIVE"
}
```

The `api_key` is returned **only once** on registration. Store it securely.

### Seed API Keys

The 4 seed banks have these pre-assigned API keys:

| Bank | BIC | API Key |
| :--- | :--- | :--- |
| Barclays | `BARCGB2L` | `ukps_chaps_key_barclays` / `ukps_fps_key_barclays` / `ukps_bacs_key_barclays` |
| HSBC | `HSBCGB44` | `ukps_chaps_key_hsbc` / `ukps_fps_key_hsbc` / `ukps_bacs_key_hsbc` |
| Lloyds | `LLOYGB21` | `ukps_chaps_key_lloyds` / `ukps_fps_key_lloyds` / `ukps_bacs_key_lloyds` |
| Alice Bank | `SNDRUK22` | `ukps_chaps_key_alice` / `ukps_fps_key_alice` / `ukps_bacs_key_alice` |

Each service has its own set of API keys. Use the scheme-specific key when
calling that service (e.g. `ukps_chaps_key_barclays` for CHAPS on port 8080).

`sort_code` is required for all three services. Scheme-specific extras:

| Service | Additional Fields |
|---|---|
| CHAPS | `sort_code` |
| FPS | `sort_code`, `participant_type` (`DIRECT`/`INDIRECT`), `sponsor_bic` |
| BACS | `sort_code`, `su_code`, `is_service_user`, `is_destination_user` |

**Sort code is required** for participant registration in all three services.
These differences reflect real UK payment scheme domain concepts:
- **Participant type + sponsor BIC** — FPS has a sponsorship model (indirect
  participants route through direct participants)
- **SU/DSU flags** — BACS distinguishes Service Users (originators) from
  Destination Service Users (receivers)

---

## Message Formats

Each scheme supports a standard interbank format. JSON is available as an
alternative for all schemes — useful for testing and internal tooling, but not
recommended for production interbank traffic.

| Scheme | Standard Format | JSON Alternative |
| :--- | :--- | :--- |
| CHAPS | ISO 20022 XML (pacs.008) | ✅ — payment submission + management |
| FPS | ISO 20022 XML (pacs.008) **or** ISO 8583 binary | ✅ — payment submission + management |
| BACS | Standard 18 (fixed-width ASCII) | ✅ — management only (cycles, mandates, reports, participants). File submission requires Standard 18. |

### ISO 20022 XML (CHAPS & FPS)

Payments arrive as a `BizMsg` envelope containing `AppHdr` + `Document`.

```xml
<BizMsg>
  <AppHdr>
    <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
    <To><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></To>
    <BizMsgIdr>BAH-REF-001</BizMsgIdr>
    <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
    <CreDt>2026-06-09T10:00:00Z</CreDt>
  </AppHdr>
  <Document>
    <FIToFICstmrCdtTrf>
      <GrpHdr>
        <MsgId>PAY-REF-001</MsgId>
      </GrpHdr>
      <CdtTrfTxInf>
        <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
        <IntrBkSttlmAmt Ccy="GBP">5000.00</IntrBkSttlmAmt>
        <DbtrAgt>
          <FinInstnId>
            <BICFI>SNDRUK22</BICFI>
            <ClrSysMmbId><MmbId>60-00-00</MmbId></ClrSysMmbId>
          </FinInstnId>
        </DbtrAgt>
        <CdtrAgt>
          <FinInstnId>
            <BICFI>BARCGB2L</BICFI>
            <ClrSysMmbId><MmbId>20-00-00</MmbId></ClrSysMmbId>
          </FinInstnId>
        </CdtrAgt>
      </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
  </Document>
</BizMsg>
```

Both CHAPS and FPS validate inbound XML against XSD schemas via libxml2
(CGO required). CHAPS uses `chaps_wrapper.xsd` for envelope validation;
FPS validates individual schemas (pacs.008, head.001, etc.).

All three schemes use UK sort codes, which are **required** in ISO 20022
XML (within `FinInstnId` > `ClrSysMmbId` > `MmbId`). Missing sort codes
are rejected with `RJCT`/`SORT-CODE-MISSING`.

Response is `pacs.002.001.16` with status: `ACTC` (settled), `PDNG` (queued),
or `RJCT` (rejected).

### ISO 8583 Binary (FPS Only)

ISO 8583 messages use 2-byte big-endian length prefix framing on TCP port
`7421`. Supported MTIs: 0200 (request) / 0210 (response).

Key data elements:

| DE | Name | Format | Example |
| :--- | :--- | :--- | :--- |
| 4 | Amount | N12 (pence) | `000000500000` (£5000.00) |
| 11 | Trace | N6 | `123456` |
| 32 | Acquirer BIC | LLVAR N..11 | `SNDRUK22` |
| 100 | Receiver BIC | LLVAR N..11 | `BARCGB2L` |

DE39 response codes: `000` (approved), `051` (insufficient funds), `057`
(not permitted).

### Standard 18 (BACS Only)

Fixed-width ASCII, 80 characters per record, newline-terminated. Record types:
1 (volume header), 3 (direct debit), 4 (direct credit), 5 (trailer), 9
(user trailer), A (AUDDIS mandate).

Monetary amounts are in pence (whole integers). The parser divides by 100.0
for GBP values. Sort codes appear in Record 3 (`DestSortCode` at positions
8–16, `OriginatorSortAcc` at 37–51) and Record 4 (`DestSortCode` at 8–16).

### JSON (All Schemes — Alternative Format)

JSON payments use the same fields across CHAPS and FPS. Sort codes (`sender_sort_code`, `receiver_sort_code`) are **required** in JSON — use ISO 8583 for sort-code-free messaging.

```json
POST /v1/payments/chaps
Content-Type: application/json
Authorization: Bearer ukps_chaps_key_alice

{
  "receiver_bic": "BARCGB2L",
  "amount": 5000.00,
  "msg_id": "PAY-REF-001",
  "receiver_sort_code": "20-00-00"
}
```

The sender BIC and sender sort code are derived from the API key —
they are **not** included in the JSON body.

The FPS JSON endpoint accepts an identical structure. BACS does **not** accept
JSON for file submission (use Standard 18) but uses JSON for all management
endpoints: cycles, mandates, returns, reports, participants, and limits.

---

## API Reference

### Common Endpoints (All Schemes)

These endpoints work identically across all three services:

| Method | Path | Description |
| :--- | :--- | :--- |
| `GET` | `/v1/healthz` | Health check (unauthenticated) |
| `GET` | `/v1/participants` | List all registered participants |
| `POST` | `/v1/participants/register` | Onboard a new bank (returns `api_key`) |
| `PATCH` | `/v1/participants/status` | Update own status (ACTIVE/SUSPENDED/DISABLED) |
| `POST` | `/v1/participants/block` | Kill-switch block (self) |
| `GET` | `/v1/participants/block` | Own block details |
| `DELETE` | `/v1/participants/block` | Unblock (self) |
| `GET` | `/v1/participants/positions` | Own real-time position |
| `POST` | `/v1/liquidity/top-up` | Central bank liquidity injection |
| `GET` | `/v1/system/schedule` | Operating hours / cycle schedule |

> All endpoints except `GET /v1/healthz` and `POST /v1/participants/register` require `Authorization: Bearer <api_key>`.

Error responses: `{"error": "message"}`. XSD validation errors apply to
CHAPS and FPS (CGO + libxml2).

### CHAPS — Payment Endpoints

| Method | Path | Description |
| :--- | :--- | :--- |
| `POST` | `/v1/payments/chaps` | Submit payment (XML or JSON) |
| `POST` | `/v1/payments/chaps/validate` | Dry-run validation |
| `GET` | `/v1/payments/chaps` | List own payments |
| `GET` | `/v1/payments/chaps/{id}` | Payment status |
| `POST` | `/v1/payments/chaps/{id}/authorize` | 2FA approval (stub) |
| `GET` | `/v1/payments/chaps/incoming` | SSE real-time stream (BIC from auth) |

### FPS — Payment Endpoints

| Method | Path | Description |
| :--- | :--- | :--- |
| `POST` | `/v1/payments/fps` | Submit SIP (XML, JSON, or ISO 8583 via HTTP) |
| `POST` | `/v1/payments/fps/iso8583` | ISO 8583 binary via HTTP |
| `POST` | `/v1/payments/fps/forward-dated` | Schedule future payment |
| `POST` | `/v1/payments/fps/standing-orders` | Create standing order |
| `POST` | `/v1/payments/fps/bulk` | Submit bulk payment file |
| `GET` | `/v1/payments/fps/incoming` | SSE real-time stream (BIC from auth) |
| `GET` | `/v1/settlement/dns/cycle` | Current DNS cycle |
| `POST` | `/v1/settlement/dns/close` | Trigger DNS settlement |

### BACS — Payment Endpoints

| Method | Path | Description |
| :--- | :--- | :--- |
| `POST` | `/v1/payments/bacs/submit` | Upload Standard 18 file |
| `GET` | `/v1/payments/bacs/cycle/current` | Current cycle info |
| `POST` | `/v1/payments/bacs/cycle/close` | Close input day |
| `POST` | `/v1/payments/bacs/cycle/settle` | Settle cycle |
| `POST` | `/v1/payments/bacs/mandates` | Create AUDDIS mandate |
| `GET` | `/v1/payments/bacs/reports/{cycle-date}` | Settlement reports |
| `GET` | `/v1/payments/bacs/incoming` | SSE real-time stream (BIC from auth) |

---

## Real-Time Events (SSE)

Each service exposes a server-sent events endpoint for real-time notifications.

| Service | Endpoint | Event Type | When |
| :--- | :--- | :--- | :--- |
| CHAPS | `GET /v1/payments/chaps/incoming` | `payment.received` | On each ACTC settlement |
| FPS | `GET /v1/payments/fps/incoming` | `payment.received` | On each ACTC settlement |
| BACS | `GET /v1/payments/bacs/incoming` | `cycle.settled` | On cycle settlement |

Authentication is required. The BIC is derived from the `Authorization` header,
so clients only receive events addressed to their own BIC.

Events are in-memory (no persistence). Clients that connect after an event
is published will not receive it. Standard SSE format:

```
data: {"type":"payment.received","data":{"msg_id":"...","amount":5000.00,...}}\n\n
```

---

## Operating Hours

Each scheme enforces operating hours at the handler level:

| Scheme | Window | Behaviour Outside Window |
| :--- | :--- | :--- |
| CHAPS | 06:00–18:00 (interbank cut-off) | HTTP 503 |
| FPS | Configurable opening/closing time | HTTP 503 or ISO 8583 DE39=91 |
| BACS | Input cut-off (configurable, default 22:30) | HTTP 503 on file submission |

BACS operates on a 3-day cycle independent of wall-clock operating hours
(file submission is gated by `input_cutoff` only).

In demo mode, cycle durations and tick intervals are compressed according to
`demo_session_minutes` in `config/sessions.json`.

---

## Error Handling

### HTTP Status Codes

| Code | Meaning |
| :--- | :--- |
| 200 | Success (settled) |
| 201 | Created (submission accepted) |
| 202 | Accepted (queued or pending) |
| 400 | Bad request (invalid format, missing fields) |
| 404 | Not found (unknown BIC, transaction, cycle) |
| 409 | Conflict (duplicate msg_id with different data) |
| 500 | Internal error |
| 503 | Service unavailable (outside operating hours) |

### Reason Codes

| Context | Code | Meaning |
| :--- | :--- | :--- |
| ISO 20022 | `ACTC` | Accepted / settled |
| ISO 20022 | `PDNG` with `INSU` | Queued — insufficient liquidity |
| ISO 20022 | `RJCT` with `AC01` | Unknown account / BIC |
| ISO 20022 | `RJCT` with `AC04` | Closed or blocked account |
| ISO 20022 | `RJCT` with `XMLI` | XSD schema invalid |
| ISO 20022 | `RJCT` with `SORT-CODE-MISSING` | Missing sort code in XML or JSON |
| ISO 8583 | DE39=`000` | Approved |
| ISO 8583 | DE39=`051` | Insufficient funds |
| ISO 8583 | DE39=`057` | Not permitted |

---

## Connection Guide

### Docker Compose

Each service has a `Dockerfile`.
The root `compose.yml` orchestrates all three services together.

```yaml
services:
  chaps-app:
    build: ./chaps-service
    ports: ["8080:8080"]
    environment:
      DATABASE_URL: postgres://chaps_admin:password123@chaps-db:5432/chaps_ledger
    depends_on: [chaps-db]

  chaps-db:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: chaps_ledger
      POSTGRES_USER: chaps_admin
      POSTGRES_PASSWORD: password123
```

### Network

Services are isolated. To connect an external bank system to a specific scheme,
place the container on the same Docker network as the target service.

### Environment Variables

| Variable | Services | Default |
| :--- | :--- | :--- |
| `DATABASE_URL` | All | Scheme-specific postgres connection string |
| `ISO8583_PORT` | FPS | `7421` |
| `PORT` | All | `8080`/`8081`/`8082` |

### Configuration

Settlement schedules, operating hours, tick intervals, and demo mode are
controlled by `config/sessions.json` in each service.

---

## Example Flows

### CHAPS — High-value RTGS payment (ISO 20022 XML)

```bash
curl -X POST http://localhost:8080/v1/payments/chaps \
  -H "Content-Type: application/xml" \
  -H "Authorization: Bearer ukps_chaps_key_alice" \
  -d '<BizMsg>
    <AppHdr>
      <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
      <To><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></To>
      <BizMsgIdr>BAH-001</BizMsgIdr>
      <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
      <CreDt>2026-06-09T10:00:00Z</CreDt>
    </AppHdr>
    <Document>
      <FIToFICstmrCdtTrf>
        <GrpHdr><MsgId>CHAPS-001</MsgId></GrpHdr>
        <CdtTrfTxInf>
          <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
          <IntrBkSttlmAmt Ccy="GBP">15000000.00</IntrBkSttlmAmt>
          <DbtrAgt>
            <FinInstnId>
              <BICFI>SNDRUK22</BICFI>
              <ClrSysMmbId><MmbId>60-00-00</MmbId></ClrSysMmbId>
            </FinInstnId>
          </DbtrAgt>
          <CdtrAgt>
            <FinInstnId>
              <BICFI>BARCGB2L</BICFI>
              <ClrSysMmbId><MmbId>20-00-00</MmbId></ClrSysMmbId>
            </FinInstnId>
          </CdtrAgt>
        </CdtTrfTxInf>
      </FIToFICstmrCdtTrf>
    </Document>
  </BizMsg>'
```

### FPS — SIP via JSON

```bash
curl -X POST http://localhost:8081/v1/payments/fps \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ukps_fps_key_alice" \
  -d '{"receiver_bic":"BARCGB2L","amount":250.00,"msg_id":"FPS-001","receiver_sort_code":"20-00-00"}'
```

### FPS — SIP via ISO 8583 TCP socket

```python
import socket, struct

def build_0200():
    buf = b'0200'
    prim = (1 << (64-4)) | (1 << (64-11)) | (1 << (64-32)) | (1 << 63)
    sec  = 1 << (128-100)
    buf += struct.pack('>Q', prim) + struct.pack('>Q', sec)
    buf += b'000000002500'         # £25.00 (2500 pence)
    buf += b'123456'               # trace
    buf += struct.pack('B', 8) + b'SNDRUK22'
    buf += struct.pack('B', 8) + b'BARCGB2L'
    return buf

s = socket.socket()
s.connect(('localhost', 7421))
msg = build_0200()
s.send(struct.pack('>H', len(msg)) + msg)
resp_len = struct.unpack('>H', s.recv(2))[0]
resp = s.recv(resp_len)
print('0210:', resp.hex())
```

### BACS — Standard 18 file upload

```bash
curl -X POST http://localhost:8082/v1/payments/bacs/submit \
  -H "Content-Type: text/plain" \
  -H "Authorization: Bearer ukps_bacs_key_barclays" \
  -d '1{SUCODE01  }0000001BARCLAYS TEST        30000100000123456'
```

### BACS — Cycle settlement lifecycle

```
Day 1: POST /v1/payments/bacs/submit          → file accepted
       POST /v1/payments/bacs/cycle/close     → OPEN → PROCESSING
Day 2: (automatic or manual advance)           → PROCESSING → AWAITING_SETTLEMENT
Day 3: POST /v1/payments/bacs/cycle/settle     → AWAITING_SETTLEMENT → SETTLED
       GET  /v1/payments/bacs/reports/{date}   → settlement report
```
