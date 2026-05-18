# CHAPS Service — Clearing House Automated Payment System

## Overview

CHAPS processes **high-value** payments in **real-time gross settlement (RTGS)**. Each payment settles individually and irrevocably across Bank of England settlement accounts. It uses **ISO 20022** messaging (pacs.008 for payment, pacs.002 for status) and operates during UK business hours only.

### CHAPS-specific concepts
- **RTGS** — Real-Time Gross Settlement: each payment settles one-by-one, no netting
- **Direct Participant** — Settlement bank with a Bank of England account (only direct participants exist in CHAPS)
- **Idempotent Settlement** — Duplicate `msg_id` submissions return cached `ACTC` without double-spending
- **2FA Authorization** — High-value payments require a second-factor approval step (simulated stub)
- **Kill-switch Block** — Immediate FCA-style block halting all outbound settlement for a participant
- **Intraday Liquidity** — Participants manage available balances throughout the day; central bank top-up simulation available
- **Clearing Limits** — Single payment cap (£20M) and daily participant cap (£100M)

### Supported Message Formats

| Format | Use Case | Details |
| :--- | :--- | :--- |
| `application/json` | GUI / admin | JSON via React dashboard |
| `application/xml` | ISO 20022 | pacs.008.001.14 (payment) wrapped in BizMsg, pacs.002.001.16 (status) response |

---

## API Layout

### Payments

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/chaps` | Initiate a new high-value CHAPS payment. Accepts ISO 20022 XML or GUI JSON payloads. | Done |
| **POST** | `/v1/payments/chaps/validate` | Dry-run validation: BIC formatting, participant status, receiver existence, liquidity availability. | Done |
| **GET** | `/v1/payments/chaps/{id}` | Retrieve settlement status, ISO 20022 details, and audit trail (journal entries) for a specific payment. | Done |
| **GET** | `/v1/payments/chaps` | List / filter payments by status (`PENDING`, `QUEUED`, `SETTLED`, `REJECTED`) and limit. | Done |
| **POST** | `/v1/payments/chaps/{id}/authorize` | Second-factor approval required for high-value release (stub). | Done |
| **DELETE** | `/v1/payments/chaps/{id}` | Cancel a payment — only allowed when status is `PENDING`. | Done |
| **POST** | `/v1/payments/chaps/{id}/amend` | Amend non-financial details of a pending payment. | Done |
| **GET** | `/v1/payments/chaps/limits` | Retrieve current clearing limits and remaining intraday liquidity (optionally per BIC). | Done |

### Participant Management

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/participants` | List all participants with BIC, name, status, balance, currency, and optional block reason. | Done |
| **POST** | `/v1/participants/register` | Onboard a new bank with BIC, name, and initial settlement balance. | Done |
| **PATCH** | `/v1/participants/{bic}/status` | Update status: `ACTIVE`, `SUSPENDED`, or `DISABLED`. | Done |
| **POST** | `/v1/participants/{bic}/block` | Immediate kill-switch. Halts all outbound settlement. Default reason: `FRAUD_SUSPECTED`. | Done |
| **GET** | `/v1/participants/{bic}/block` | Block details: status, `blocked_at` timestamp, reason, operator. | Done |
| **DELETE** | `/v1/participants/{bic}/block` | Unblock and restore settlement rights (sets status to `ACTIVE`). | Done |
| **GET** | `/v1/participants/{bic}/positions` | Real-time position: `balance`, `earmarked`, `available`. | Done |

### Liquidity

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/liquidity/top-up` | Simulation only: injects funds into a bank's settlement account (central bank operation). | Done |

### System Metadata

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/system/schedule` | Today's cut-off times: opening (`06:00`), customer cut-off (`17:40`), interbank cut-off (`18:00`). | Done |

---

## ISO 20022 Message Format

### Inbound (pacs.008.001.14)

Payments arrive as XML wrapped in a custom `BizMsg` envelope containing `AppHdr` + `Document`.

```
POST /v1/payments/chaps
Content-Type: application/xml

<BizMsg>
  <AppHdr>
    <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
    <To><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></To>
    <BizMsgIdr>BAH-REF-001</BizMsgIdr>
    <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
    <CreDt>2026-05-18T14:30:00Z</CreDt>
  </AppHdr>
  <Document>
    <FIToFICstmrCdtTrf>
      <GrpHdr>
        <MsgId>CHAPS-20260518-001</MsgId>
      </GrpHdr>
      <CdtTrfTxInf>
        <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
        <IntrBkSttlmAmt Ccy="GBP">15000000.00</IntrBkSttlmAmt>
        <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
        <CdtrAgt><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></CdtrAgt>
      </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
  </Document>
</BizMsg>
```

### Validation & Parsing Flow

```
Inbound XML → chaps_wrapper.xsd envelope validation (libxml2)
           → XPath: extract MsgDefIdr → route to pacs.008 handler
           → XPath: extract <Document> inner XML
           → xml.Unmarshal into Pacs008Message struct
           → Ledger.SettlePayment() — transactional gross settlement
           → Generate Pacs002Message response
           → Wrap in BusinessMessage{AppHdr + Document}
           → Return application/xml
```

### Response (pacs.002.001.16)

| Status | Meaning | HTTP Code | Reason Code |
| :--- | :--- | :--- | :--- |
| `ACTC` | Accepted / Settled | 200 | — |
| `PDNG` | Pending / Queued (insufficient liquidity) | 202 | `INSU` |
| `RJCT` | Rejected | 202 | `AC01` (unknown account), `AC04` (closed/blocked), `XMLI` (schema invalid), `PARSE-ERR` |

Response headers include `X-Transaction-Status` for quick inspection.

### Business Application Header (head.001.001.04)

The `AppHdr` is generated from `NewBAH(from, to, msgId, msgType)` with:
- `BizMsgIdr` prefixed `BAH-`
- `MsgDefIdr` set to `pacs.002.001.16` for responses
- `CreDt` set to current time (second precision)
- Optional `Sgntr` placeholder for future XMLDSig

---

## RTGS Settlement

CHAPS uses **real-time gross settlement**: each payment instruction is processed individually and settled immediately (subject to liquidity).

### Settlement Logic (SettlePayment)

```
1. Insert transaction (PENDING) with ON CONFLICT (msg_id) idempotency gate
   → If already SETTLED: return cached ACTC immediately

2. Lock sender's status row (FOR UPDATE)
   → Reject if sender is not ACTIVE or is_closed

3. Lock sender's liquidity row (FOR UPDATE)
   → If insufficient balance: set status to QUEUED, return PDNG

4. Execute gross settlement:
   → Debit sender: balance -= amount
   → Credit receiver: balance += amount

5. Record immutable journal entries (2 rows):
   → Sender entry: negative amount (debit)
   → Receiver entry: positive amount (credit)
   → pg_notify('liquidity_event', receiver_bic) triggered on insert

6. Finalize: SETTLED
```

### Idempotency

```sql
INSERT INTO transactions (msg_id, sender_bic, receiver_bic, amount, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
RETURNING id, status
```

If `status` is already `SETTLED`, the transaction is skipped and cached `ACTC` returned. This prevents double-spending on retransmissions.

### Clearing Limits

| Limit | Value |
| :--- | :--- |
| Currency | GBP |
| Single Payment Limit | £20,000,000 |
| Daily Participant Limit | £100,000,000 |

---

## Database Tables

### 1. `participant_profiles` — Static profile data

| Column | Type | Notes |
| :--- | :--- | :--- |
| `bic_code` | `VARCHAR(11) PK` | BIC identifier |
| `name` | `TEXT NOT NULL` | Bank name |
| `currency` | `VARCHAR(3) DEFAULT 'GBP'` | Settlement currency |
| `created_at` | `TIMESTAMPTZ DEFAULT NOW()` | Onboarding timestamp |

### 2. `participant_liquidity` — High-frequency balance updates

| Column | Type | Notes |
| :--- | :--- | :--- |
| `bic_code` | `VARCHAR(11) PK` | References `participant_profiles` |
| `balance` | `DECIMAL(20, 2) NOT NULL` | Current settlement balance |
| `updated_at` | `TIMESTAMPTZ DEFAULT NOW()` | Last update timestamp |

### 3. `participant_statuses` — Risk & controls

| Column | Type | Notes |
| :--- | :--- | :--- |
| `bic_code` | `VARCHAR(11) PK` | References `participant_profiles` |
| `status` | `ENUM(ACTIVE, SUSPENDED, DISABLED)` | Operational status |
| `is_closed` | `BOOLEAN DEFAULT FALSE` | Account closure flag |
| `blocked_at` | `TIMESTAMPTZ` | When the kill-switch was triggered |
| `block_reason` | `TEXT` | Reason for block |
| `updated_at` | `TIMESTAMPTZ DEFAULT NOW()` | Last update timestamp |

### 4. `transactions` — Payment records

| Column | Type | Notes |
| :--- | :--- | :--- |
| `id` | `UUID PK DEFAULT uuidv7()` | Postgres 18 native UUID v7 |
| `msg_id` | `VARCHAR(35) UNIQUE` | Client-provided message ID (idempotency key) |
| `sender_bic` | `VARCHAR(11)` | Debtor bank BIC |
| `receiver_bic` | `VARCHAR(11)` | Creditor bank BIC |
| `amount` | `DECIMAL(20, 2) NOT NULL` | Settlement amount |
| `status` | `ENUM(PENDING, QUEUED, SETTLED, REJECTED)` | Current state |
| `created_at` | `TIMESTAMPTZ DEFAULT NOW()` | Creation timestamp |

### 5. `journal_entries` — Immutable audit trail

| Column | Type | Notes |
| :--- | :--- | :--- |
| `id` | `SERIAL PK` | Auto-incrementing |
| `transaction_id` | `UUID` | References `transactions(id)` |
| `account_bic` | `VARCHAR(11)` | Affected participant BIC |
| `amount` | `DECIMAL(20, 2) NOT NULL` | Negative = debit, Positive = credit |
| `entry_date` | `TIMESTAMPTZ DEFAULT NOW()` | When the entry was recorded |

A trigger on `journal_entries` fires `pg_notify('liquidity_event', NEW.account_bic)` for credit entries (positive amount), enabling real-time balance notifications.

---

## Seed Data

| BIC | Name | Initial Balance |
| :--- | :--- | :--- |
| `BARCGB2L` | Barclays Bank | £1,000,000.00 |
| `HSBCGB44` | HSBC UK | £500,000.00 |
| `LLOYGB21` | Lloyds Bank | £750,000.00 |
| `SNDRUK22` | Alice Bank | £1,000,000.00 |

---

## Operating Schedule

```
Opening Time:        06:00 (Europe/London)
Customer Cut-off:    17:40
Interbank Cut-off:   18:00
```

---

## Directory Structure

```
chaps-service/
├── cmd/server/main.go            # Bootstrap (DB pool → validator registry → server → HTTP listener)
├── internal/db/
│   ├── 01_init.sql               # Schema: 5 tables, custom enums, journal trigger
│   └── 02_seed.sql               # Seed data: 4 banks with balances
├── pkg/server/server.go          # HTTP router + 17 handlers, JSON/XML dispatch
├── pkg/ledger/service.go         # Settlement engine, participant management, positions, limits
├── pkg/iso20022/
│   ├── bah.go                    # Business Application Header (AppHdr) struct + constructor
│   ├── pacs008.go                # pacs.008 payment message struct (XML tags)
│   └── pacs002.go                # pacs.002 status response struct + NewPacs002 constructor
├── pkg/validator/validator.go    # XSD schema registry, envelope validation, XPath extraction
├── xsd/
│   ├── chaps_wrapper.xsd         # Custom wrapper envelope (BizMsg)
│   ├── head.001.001.02.xsd       # ISO 20022 BAH v02
│   ├── head.001.001.04.xsd       # ISO 20022 BAH v04
│   ├── pacs.008.001.14.xsd       # ISO 20022 payment message
│   ├── pacs.002.001.16.xsd       # ISO 20022 status report
│   ├── pacs.009.001.13.xsd       # Unused — financial institution credit transfer
│   └── pacs.029.001.02.xsd       # Unused — resolution of investigation
├── test/
│   ├── test_good.xml             # Valid pacs.008 (bare document)
│   ├── test_bad.xml              # Invalid pacs.008 (schema violation)
│   ├── test_wrapped.xml          # Full BizMsg envelope (AppHdr + Document)
│   └── test_wrapped2.xml         # Alternative BizMsg variant
├── web/chaps-gui/                # React 18 + Vite 5 + TypeScript 5 operator dashboard
│   ├── src/
│   │   ├── App.tsx               # Main dashboard UI (Polish labels)
│   │   ├── api.ts                # Generic request<T>() API client
│   │   ├── types.ts              # TypeScript interfaces matching Go JSON tags
│   │   ├── main.tsx              # React mount
│   │   └── styles.css            # Full responsive stylesheet
│   ├── index.html
│   ├── vite.config.ts            # Dev proxy: /v1 → localhost:8080
│   └── package.json              # React 18, Vite 5, TypeScript 5
├── Dockerfile                    # Multi-stage: golang:1.26-alpine → alpine:3.23, static CGO
├── compose.yml                   # Production: db (Postgres 18) + app (port 8080)
├── compose-dev.yml               # Dev: db only (app runs on host)
├── go.mod / go.sum
└── README.md
```
