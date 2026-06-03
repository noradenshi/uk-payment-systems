# FPS Service — Faster Payments

> 📖 Dokumentacja integracyjna: [docs/fps/integration/INFO.md](../docs/fps/integration/INFO.md)

## Overview

FPS (Faster Payments Service) processes near-real-time low-value payments 24/7. It supports dual-format messaging: **ISO 20022** (XML, modern) and **ISO 8583** (binary, legacy). Settlement is deferred net settlement (DNS) with optional prefunding for direct participants.

### FPS-specific concepts
- **SIP** — Single Immediate Payment (settles instantly if liquidity permits)
- **Forward Dated** — Scheduled for future execution
- **Standing Order** — Recurring instruction (daily/weekly/monthly)
- **Bulk Payment** — File of many payments, netted together
- **Direct Participant** — Settlement bank within the FPS scheme
- **Indirect Participant** — Sponsored by a direct participant for access
- **DNS** — Deferred Net Settlement: net positions calculated and settled in batches throughout the day

### Supported Message Formats

| Format | Use Case | File Type |
| :--- | :--- | :--- |
| `application/json` | GUI / admin | JSON |
| `application/xml` | ISO 20022 | pacs.008 (payment), pacs.002 (status) |
| `application/octet-stream` | ISO 8583 | Binary MTI 0200/0210 messages |

---

## API Layout

### Payments — Single Immediate Payment (SIP)

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps` | Initiate a SIP. Accepts ISO 20022 XML, ISO 8583 binary, or JSON. | Done |
| **POST** | `/v1/payments/fps/validate` | Dry-run validation: BIC, status, liquidity, format compliance. | Done |
| **GET** | `/v1/payments/fps/{id}` | Retrieve settlement status, ISO 20022/8583 details, and audit trail. | Done |
| **GET** | `/v1/payments/fps` | List/filter FPS payments by status, date range, participant, limit. | Done |
| **DELETE** | `/v1/payments/fps/{id}` | Recall a payment (only if not yet settled). | Done |

### Payments — Forward Dated

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/forward-dated` | Schedule a future-dated payment. | Done |
| **GET** | `/v1/payments/fps/forward-dated` | List scheduled forward-dated payments. | Done |
| **DELETE** | `/v1/payments/fps/forward-dated/{id}` | Cancel a scheduled forward-dated payment before execution. | Done |

### Payments — Standing Orders

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/standing-orders` | Create a recurring standing order. | Done |
| **GET** | `/v1/payments/fps/standing-orders` | List standing orders for a participant. | Done |
| **GET** | `/v1/payments/fps/standing-orders/{id}` | Get standing order details and execution history. | Done |
| **PATCH** | `/v1/payments/fps/standing-orders/{id}` | Amend amount/frequency/end-date of a standing order. | Done |
| **DELETE** | `/v1/payments/fps/standing-orders/{id}` | Cancel a standing order. | Done |

### Payments — Bulk

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/bulk` | Submit a bulk payment file (ISO 20022 XML or Standard 18-like CSV). | Done |
| **GET** | `/v1/payments/fps/bulk/{id}` | Get bulk submission status and per-item breakdown. | Done |
| **GET** | `/v1/payments/fps/bulk` | List bulk submissions. | Done |

### Participant Management

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/participants` | List participants. | Done |
| **POST** | `/v1/participants/register` | Onboard a participant with BIC, name, settlement type (DIRECT/INDIRECT), sponsor BIC. | Done |
| **PATCH** | `/v1/participants/{bic}/status` | Update status (ACTIVE/SUSPENDED/DISABLED). | Done |
| **POST** | `/v1/participants/{bic}/block` | Kill-switch block. | Done |
| **GET** | `/v1/participants/{bic}/block` | Block details. | Done |
| **DELETE** | `/v1/participants/{bic}/block` | Unblock. | Done |
| **GET** | `/v1/participants/{bic}/positions` | Real-time position (prefunded balance + DNS net position). | Done |

### Settlement & Liquidity

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/settlement/dns/cycle` | Current DNS cycle details (net position per participant, settlement time). | Done |
| **POST** | `/v1/settlement/dns/close` | Trigger DNS cycle close — calculate net positions, settle. | Done |
| **GET** | `/v1/settlement/dns/history` | Historical DNS cycle settlements. | Done |
| **POST** | `/v1/liquidity/top-up` | Simulate prefunding or central bank injection. | Done |
| **GET** | `/v1/liquidity/prefunded/{bic}` | Get prefunded balance for a participant. | Done |

### Limits & Controls

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/fps/limits` | FPS-specific limits (max single payment, daily cumulative, participant cap). | Done |
| **PATCH** | `/v1/payments/fps/limits/{bic}` | Update per-participant FPS limits. | Done |

### Notifications

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/fps/incoming/{bic}` | SSE real-time stream of incoming payment notifications (`payment.received` events). | Done |

### System Metadata

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/system/schedule` | FPS operating schedule (always 24/7, but may include maintenance windows). | Done |

---

## Message Format: ISO 20022

Inbound flow mirrors CHAPS:
```
POST /v1/payments/fps
Content-Type: application/xml

<AppHdr>...</AppHdr>
<Document>
  <FIToFICstmrCdtTrf>
    <GrpHdr>
      <MsgId>FPS-20260518-001</MsgId>
    </GrpHdr>
    <CdtTrfTxInf>
      <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
      <IntrBkSttlmAmt Ccy="GBP">500.00</IntrBkSttlmAmt>
      <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
      <CdtrAgt><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></CdtrAgt>
    </CdtTrfTxInf>
  </FIToFICstmrCdtTrf>
</Document>
```

Response is `pacs.002.001.16` with status:
- `ACTC` — Accepted (settled)
- `PDNG` — Pending (liquidity check queued for DNS)
- `RJCT` — Rejected

**Note**: FPS does not use XSD validation (CGO-free). XML is parsed directly via Go's `encoding/xml`.

---

## Message Format: ISO 8583

ISO 8583 uses binary (or ASCII/BCD) messages with a fixed MTI header.

### Supported MTIs
| MTI | Name | Direction |
| :--- | :--- | :--- |
| 0200 | Financial Transaction Request | Client → FPS |
| 0210 | Financial Transaction Response | FPS → Client |
| 0400 | Reversal Request | Client → FPS |
| 0410 | Reversal Response | FPS → Client |
| 0800 | Network Management Request | Client → FPS |
| 0810 | Network Management Response | FPS → Client |

### Bitmap Layout (DE = Data Element)

| DE | Name | Format | Example |
| :--- | :--- | :--- | :--- |
| 2 | PAN / Account Number | LLVAR N..19 | `1678901234567890` |
| 3 | Processing Code | N6 | `100000` (GBP credit) |
| 4 | Amount Transaction | N12 | `000000050000` (£500.00) |
| 7 | Transmission Date & Time | N10 (MMDDhhmmss) | `0518142530` |
| 11 | System Trace Audit Number | N6 | `123456` |
| 12 | Local Time | N6 (hhmmss) | `142530` |
| 13 | Local Date | N4 (MMDD) | `0518` |
| 32 | Acquiring Institution ID | LLVAR N..11 | `SNDRUK22` |
| 37 | Retrieval Reference Number | AN12 | `REF123456789` |
| 41 | Terminal ID | ANS8 | `FPSGW01` |
| 42 | Card Acceptor ID | ANS15 | `UKFPSACCEPTOR` |
| 49 | Currency Code | N3 | `826` (GBP) |
| 100 | Receiving Institution ID | LLVAR N..11 | `BARCGB2L` |
| 102 | Account ID 1 (Sender) | LLVAR ANS..28 | `SNDRUK22ACCT` |
| 103 | Account ID 2 (Receiver) | LLVAR ANS..28 | `BARCGB2LACCT` |

### Processing (Go integration)
```go
// Decode ISO 8583 binary message
type ISO8583_0200 struct {
    MTI              string // Positions 0-3: "0200"
    PrimaryBitmap    uint64 // 8 bytes
    SecondaryBitmap  uint64 // 8 bytes (optional)
    DE2_PAN          string
    DE3_ProcCode     string
    DE4_Amount       int64  // Minor units (pence)
    DE7_TransDateTime string
    DE11_Trace       int
    DE32_Acquirer    string
    DE100_Receiver   string
}
```

On receipt of an 0200 message, the service:
1. Parses the bitmap to extract DEs
2. Validates BIC in DE32/DE100 exist and are ACTIVE
3. Checks liquidity (prefunded for FPS)
4. Debits DE32, credits DE100
5. Returns 0210 with DE39 (response code): `000` (approved), `051` (insufficient funds), `057` (not permitted)

---

## ISO 8583 Endpoints

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/iso8583` | Accept raw ISO 8583 binary (`application/octet-stream`). Returns 0210 binary. | Done |
| **GET** | `/v1/payments/fps/iso8583/decode` | Utility: decode an ISO 8583 message and return human-readable JSON for debugging. | Done |

The content-type dispatch in the main `POST /v1/payments/fps` handler routes ISO 8583 via:
```go
case "application/octet-stream":
    s.processISO8583Payment(w, r)
```

### TCP Socket (realistic transport)

In addition to the HTTP endpoint, the FPS service listens on a **raw TCP socket** for ISO 8583 messages, simulating a real payment switch connection.

| Setting | Value |
| :--- | :--- |
| Default port | `7421` |
| Config via | `ISO8583_PORT` env var (e.g. `:7421`) |
| Framing | 2-byte big-endian length prefix, then the ISO 8583 binary message |
| Response | 2-byte big-endian length prefix, then 0210 binary response |
| Max message | 4096 bytes |

**Protocol flow:**
```
Client                            Server (:7421)
  │                                  │
  ├── [2 bytes: uint16 length N] ───>│
  ├── [N bytes: 0200 + bitmap + DEs]─>
  │                                  ├── ParseISO8583
  │                                  ├── Ledger.SettleSIP
  │                                  ├── SSE event publish
  │                                  │
  │<── [2 bytes: uint16 length M] ───┤
  │<── [M bytes: 0210 + bitmap + DEs]─┤
```

Each connection handles one request-response exchange and closes. Goroutine-per-connection with a 10-second ledger timeout.

**Usage example (Go):**
```go
conn, _ := net.Dial("tcp", "localhost:7421")
defer conn.Close()

// Build ISO 8583 0200 binary
body := encode0200("SNDRUK22", "BARCGB2L", 5000, 123456)
binary.Write(conn, binary.BigEndian, uint16(len(body)))
conn.Write(body)

// Read response length-prefixed 0210
var respLen uint16
binary.Read(conn, binary.BigEndian, &respLen)
resp := make([]byte, respLen)
io.ReadFull(conn, resp)
fmt.Printf("0210: %x\n", resp)

func encode0200(acq, recv string, amtPence int64, trace int) []byte {
    var buf []byte
    buf = append(buf, []byte("0200")...)
    prim := uint64(1<<(64-4)) | uint64(1<<(64-11)) | uint64(1<<(64-32)) | uint64(1<<63)
    sec  := uint64(1 << (128 - 100))
    b := make([]byte, 8)
    binary.BigEndian.PutUint64(b, prim); buf = append(buf, b...)
    binary.BigEndian.PutUint64(b, sec);  buf = append(buf, b...)
    buf = append(buf, fmt.Sprintf("%012d", amtPence)...)
    buf = append(buf, fmt.Sprintf("%06d", trace)...)
    buf = append(buf, byte(len(acq))); buf = append(buf, []byte(acq)...)
    buf = append(buf, byte(len(recv))); buf = append(buf, []byte(recv)...)
    return buf
}
```

**Usage example (bash via `nc`):**
```bash
# Use a helper script (e.g. Python or Go tool) to build the binary,
# or pipe pre-encoded hex:
echo '023030303030303030303030303030303035303030303132333435363808534e4452554b323208424152434742324c' \
  | xxd -r -p \
  | python3 -c "
import sys, struct
data = sys.stdin.buffer.read()
sys.stdout.buffer.write(struct.pack('>H', len(data)) + data)
" \
  | nc -q1 localhost 7421 | xxd
```

**Python test client:**
```python
import socket, struct

def build_0200():
    buf = b'0200'
    # bitmap: bits 4, 11, 32, 100
    prim = (1 << (64-4)) | (1 << (64-11)) | (1 << (64-32)) | (1 << 63)
    sec  = 1 << (128-100)
    buf += struct.pack('>Q', prim)
    buf += struct.pack('>Q', sec)
    buf += b'000000005000'       # DE4: 50.00 GBP (5000 pence)
    buf += b'123456'             # DE11: trace
    buf += struct.pack('B', 8) + b'SNDRUK22'   # DE32: acquirer (LLVAR)
    buf += struct.pack('B', 8) + b'BARCGB2L'   # DE100: receiver (LLVAR)
    return buf

s = socket.socket()
s.connect(('localhost', 7421))
msg = build_0200()
s.send(struct.pack('>H', len(msg)) + msg)
resp_len = struct.unpack('>H', s.recv(2))[0]
resp = s.recv(resp_len)
print('0210 response:', resp.hex())
```

---

## Settlement: DNS (Deferred Net Settlement)

FPS uses DNS — payments are accepted and queued throughout the day. At predefined intervals:

1. **Net position calculation** — for each direct participant: sum of all outbound minus inbound SIPs since last cycle
2. **Reporting** — each participant gets their net position
3. **Settlement** — participants with negative net positions pay into the settlement account; participants with positive net positions receive funds

```
Cycle 1:  08:00 - 10:00  →  settle at 10:15
Cycle 2:  10:00 - 12:00  →  settle at 12:15
Cycle 3:  12:00 - 14:00  →  settle at 14:15
Cycle 4:  14:00 - 16:00  →  settle at 16:15
Cycle 5:  16:00 - 18:00  →  settle at 18:15
```

---

## Database Tables (FPS-specific)

Beyond the shared participant tables (`participant_profiles`, `participant_liquidity`, `participant_statuses`):

| Table | Purpose |
| :--- | :--- |
| `fps_transactions` | SIP records with msg_id unique constraint |
| `fps_forward_dated` | Scheduled future payments with execution_date |
| `fps_standing_orders` | Recurring instructions (frequency, amount, counters) |
| `fps_bulk_submissions` | Batch file metadata and per-item status |
| `fps_dns_cycles` | DNS cycle state (net positions per participant, settled flag) |
| `fps_journal_entries` | Immutable audit trail (shared schema pattern) |

---

## Testing

| Package | File | Tests | Description |
| :--- | :--- | :--- | :--- |
| `pkg/iso20022` | `serialization_test.go` | 12 | pacs.008 unmarshal, pacs.002 marshal (ACTC/RJCT/PDNG), BAH construction, envelope wrapping, round-trip, reason codes, timestamps |
| `pkg/iso8583` | `message_test.go` | 7 | 0200 parse, MTI validation, optional fields, amount+trace, 0210 encode, round-trip, full field parse |

Run all tests:

```bash
go test ./pkg/... -v -count=1
```

---

## Directory Structure

```
fps-service/
├── cmd/server/main.go          # Bootstrap (DB → server → HTTP listener)
├── internal/db/
│   ├── 01_init.sql             # Full schema: 9 tables, custom enums
│   └── 02_seed.sql             # 4 banks + open DNS cycle
├── pkg/server/server.go        # 32 endpoints, JSON/XML/ISO 8583 dispatch, SSE streaming
├── pkg/events/events.go        # In-memory EventBus for SSE real-time notifications
├── pkg/ledger/service.go       # SIP settlement, DNS, limits, gridlock
├── pkg/iso20022/
│   ├── bah.go                  # Business Application Header
│   ├── pacs008.go              # pacs.008 payment struct
│   ├── pacs002.go              # pacs.002 status response struct
│   └── serialization_test.go   # 12 tests
├── pkg/iso8583/
│   ├── message.go              # 0200/0210 structs, bitmap parsing (221 lines)
│   └── message_test.go         # 7 tests
├── Dockerfile                  # Multi-stage, CGO_ENABLED=0
├── compose.yml                 # Production: db + app (port 8081)
├── compose-dev.yml             # Dev: db only
├── go.mod / go.sum
└── README.md
```
