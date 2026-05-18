# FPS Service — Faster Payments

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
| **POST** | `/v1/payments/fps` | Initiate a SIP. Accepts ISO 20022 XML, ISO 8583 binary, or JSON. | Design |
| **POST** | `/v1/payments/fps/validate` | Dry-run validation: BIC, status, liquidity, format compliance. | Design |
| **GET** | `/v1/payments/fps/{id}` | Retrieve settlement status, ISO 20022/8583 details, and audit trail. | Design |
| **GET** | `/v1/payments/fps` | List/filter FPS payments by status, date range, participant, limit. | Design |
| **DELETE** | `/v1/payments/fps/{id}` | Recall a payment (only if not yet settled). | Design |

### Payments — Forward Dated

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/forward-dated` | Schedule a future-dated payment. | Design |
| **GET** | `/v1/payments/fps/forward-dated` | List scheduled forward-dated payments. | Design |
| **DELETE** | `/v1/payments/fps/forward-dated/{id}` | Cancel a scheduled forward-dated payment before execution. | Design |

### Payments — Standing Orders

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/standing-orders` | Create a recurring standing order. | Design |
| **GET** | `/v1/payments/fps/standing-orders` | List standing orders for a participant. | Design |
| **GET** | `/v1/payments/fps/standing-orders/{id}` | Get standing order details and execution history. | Design |
| **PATCH** | `/v1/payments/fps/standing-orders/{id}` | Amend amount/frequency/end-date of a standing order. | Design |
| **DELETE** | `/v1/payments/fps/standing-orders/{id}` | Cancel a standing order. | Design |

### Payments — Bulk

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/fps/bulk` | Submit a bulk payment file (ISO 20022 XML or Standard 18-like CSV). | Design |
| **GET** | `/v1/payments/fps/bulk/{id}` | Get bulk submission status and per-item breakdown. | Design |
| **GET** | `/v1/payments/fps/bulk` | List bulk submissions. | Design |

### Participant Management

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/participants` | List participants. | Design |
| **POST** | `/v1/participants/register` | Onboard a participant with BIC, name, settlement type (DIRECT/INDIRECT), sponsor BIC. | Design |
| **PATCH** | `/v1/participants/{bic}/status` | Update status (ACTIVE/SUSPENDED/DISABLED). | Design |
| **POST** | `/v1/participants/{bic}/block` | Kill-switch block. | Design |
| **GET** | `/v1/participants/{bic}/block` | Block details. | Design |
| **DELETE** | `/v1/participants/{bic}/block` | Unblock. | Design |
| **GET** | `/v1/participants/{bic}/positions` | Real-time position (prefunded balance + DNS net position). | Design |

### Settlement & Liquidity

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/settlement/dns/cycle` | Current DNS cycle details (net position per participant, settlement time). | Design |
| **POST** | `/v1/settlement/dns/close` | Trigger DNS cycle close — calculate net positions, settle. | Design |
| **GET** | `/v1/settlement/dns/history` | Historical DNS cycle settlements. | Design |
| **POST** | `/v1/liquidity/top-up` | Simulate prefunding or central bank injection. | Design |
| **GET** | `/v1/liquidity/prefunded/{bic}` | Get prefunded balance for a participant. | Design |

### Limits & Controls

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/fps/limits` | FPS-specific limits (max single payment, daily cumulative, participant cap). | Design |
| **PATCH** | `/v1/payments/fps/limits/{bic}` | Update per-participant FPS limits. | Design |

### System Metadata

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/system/schedule` | FPS operating schedule (always 24/7, but may include maintenance windows). | Design |

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

**XSDs required**: `pacs.008.001.14`, `pacs.002.001.16`, `head.001.001.04`, `fps_wrapper.xsd` (custom envelope).

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
| **POST** | `/v1/payments/fps/iso8583` | Accept raw ISO 8583 binary (`application/octet-stream`). Returns 0210 binary. | Design |
| **GET** | `/v1/payments/fps/iso8583/decode` | Utility: decode an ISO 8583 message and return human-readable JSON for debugging. | Design |

The content-type dispatch in the main `POST /v1/payments/fps` handler routes ISO 8583 via:
```go
case "application/octet-stream":
    s.processISO8583Payment(w, r)
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

## Proposed Directory Structure

```
fps-service/
├── cmd/server/main.go          # Bootstrap (DB → validator → server)
├── internal/db/
│   ├── 01_init.sql             # FPS-specific tables
│   └── 02_seed.sql             # Seed direct/indirect participants
├── pkg/server/server.go        # Routes + handlers
├── pkg/ledger/service.go       # Settlement, DNS, limits
├── pkg/iso20022/               # Reuse pacs.008/pacs.002 models from chaps-service or own
├── pkg/iso8583/                # ISO 8583 message encode/decode
│   ├── message.go              # 0200/0210 structs, bitmap helpers
│   └── fields.go               # DE definitions
├── xsd/                        # ISO 20022 XSDs (reuse shared or symlink)
├── test/                       # Test payloads
├── web/fps-gui/                # Optional React dashboard
├── Dockerfile
├── compose.yml
├── compose-dev.yml
└── README.md
```
