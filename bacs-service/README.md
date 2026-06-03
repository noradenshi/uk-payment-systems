# BACS Service — Bankers Automated Clearing Services

> 📖 Dokumentacja integracyjna: [docs/bacs/integration/INFO.md](../docs/bacs/integration/INFO.md)

## Overview

BACS processes **batch** low-value payments on a **3-day settlement cycle**. It uses the **Standard 18** fixed-width format for file submission via BACSTEL-IP. Unlike CHAPS (RTGS) and FPS (near-real-time), BACS is store-and-forward: files are submitted, validated, processed overnight, and settled on day 3.

### The 3-Day Cycle

```
Day 1 (Input Day):   Files submitted before 22:30 cut-off. Accepted or rejected.
Day 2 (Processing):  Files are sorted and net positions calculated. No settlement.
Day 3 (Settlement):  Net amounts are debited/credited. Funds available by 07:00.
```

### BACS-specific concepts
- **Service User (SU)** — An originator of Direct Debits or Direct Credits (e.g. a utility company)
- **Destination Service User (DSU)** — The receiving organisation
- **Standard 18** — Fixed-width record format, 18 record types
- **AUDDIS** — Automated Direct Debit Instruction Service (mandate management)
- **ARUDD** — Automated Return of Unpaid Direct Debits (returns)
- **BACSTEL-IP** — The submission protocol (FTP / HTTPS file upload)
- **Volume** — Total count of transactions in a submission
- **Value** — Total monetary value of a submission or cycle

### Supported Message Format

| Format | Use Case | Details |
| :--- | :--- | :--- |
| `application/json` | GUI / admin | JSON |
| `text/plain` | Standard 18 file upload | Fixed-width ASCII, 80 chars per record |
| `multipart/form-data` | File + metadata in one request | Standard 18 file attached |

---

## API Layout

### File Submission

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/bacs/submit` | Upload a Standard 18 file. Accepts `text/plain` body or `multipart/form-data` with file. Returns submission receipt with SU, volume, value, hash. | Done |
| **GET** | `/v1/payments/bacs/submit/{id}` | Get submission status, volume, value, error count. | Done |
| **GET** | `/v1/payments/bacs/submit` | List submissions filtered by status, SU, date range. | Done |
| **DELETE** | `/v1/payments/bacs/submit/{id}` | Recall a submission before input day cut-off. | Done |

### Cycle Management

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/bacs/cycle/current` | Get current cycle info: input day date, processing day date, settlement day date, cut-off time. | Done |
| **GET** | `/v1/payments/bacs/cycle/{cycle-date}` | Get a specific settlement cycle (e.g. `/cycle/2026-05-20`). | Done |
| **GET** | `/v1/payments/bacs/cycle` | List past cycles (recent 30 days). | Done |
| **POST** | `/v1/payments/bacs/cycle/close` | **Operator only**: force-close input day and lock further submissions for this cycle. | Done |
| **POST** | `/v1/payments/bacs/cycle/process` | **Operator only**: advance cycle from PROCESSING to AWAITING_SETTLEMENT. | Done |
| **POST** | `/v1/payments/bacs/cycle/settle` | **Operator only**: settle all net positions and finalise the cycle. | Done |
| **GET** | `/v1/payments/bacs/incoming/{bic}` | SSE real-time stream of cycle settlement notifications (`cycle.settled` events). | Done |

### Service User Management

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/participants/register` | Onboard a participant with BIC, name, SU/DSU flag, sponsor BIC. | Done |
| **GET** | `/v1/participants` | List participants. | Done |
| **PATCH** | `/v1/participants/{bic}/status` | Update status (ACTIVE/SUSPENDED/DISABLED). | Done |
| **POST** | `/v1/participants/{bic}/block` | Kill-switch block. | Done |
| **GET** | `/v1/participants/{bic}/block` | Block details. | Done |
| **DELETE** | `/v1/participants/{bic}/block` | Unblock. | Done |

### Direct Debit Mandates (AUDDIS)

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/bacs/mandates` | Create a new Direct Debit mandate (AUDDIS instruction). | Done |
| **GET** | `/v1/payments/bacs/mandates/{ref}` | Get mandate details, status, history. | Done |
| **GET** | `/v1/payments/bacs/mandates` | List mandates for a SU/DSU. | Done |
| **PATCH** | `/v1/payments/bacs/mandates/{ref}` | Amend mandate (amount, frequency, dates). | Done |
| **DELETE** | `/v1/payments/bacs/mandates/{ref}` | Cancel a mandate (AUDDIS cancellation). | Done |
| **POST** | `/v1/payments/bacs/mandates/{ref}/claim` | Submit a mandate claim against a specific account. | Done |

### Returns & Rejects (ARUDD)

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/bacs/returns` | List ARUDD returns (unpaid Direct Debits). | Done |
| **POST** | `/v1/payments/bacs/returns` | Submit an ARUDD return instruction. | Done |
| **GET** | `/v1/payments/bacs/rejects` | List submission rejects (file format errors or validation failures). | Done |
| **GET** | `/v1/payments/bacs/reports/{cycle-date}` | End-of-cycle settlement reports per SU (DD/DC totals, net positions). | Done |

### Reports

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/bacs/reports/{cycle-date}/su/{bic}` | Per-SU settlement report for a given cycle. | Done |
| **GET** | `/v1/payments/bacs/reports/{cycle-date}/summary` | Cycle summary: total volume, total value, net positions, error rates. | Done |
| **GET** | `/v1/payments/bacs/reports/su/{bic}` | Historical reports for a specific Service User. | Done |

### Limits & Controls

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/payments/bacs/limits` | BACS limits (max file size, max per-transaction, SU daily caps). | Done |
| **PATCH** | `/v1/payments/bacs/limits/{bic}` | Update per-participant BACS limits. | Done |

### System Metadata

| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **GET** | `/v1/system/schedule` | Returns upcoming cycle dates (input/processing/settlement days for the next N cycles). | Done |

---

## Standard 18 Format

Standard 18 is a **fixed-width** record format with exactly **80 characters per line**, newline-terminated. 18 record types exist (1–9, plus A–I). The most common are:

### Record Types

| Type | Name | Mandatory | Purpose |
| :--- | :--- | :--- | :--- |
| `1` | Volume Header | Yes | File-level: SU, DSU, date, total value, total volume |
| `2` | Output Spec | Yes | Bank/branch sort codes for the DSU |
| `3` | Direct Debit Input | Conditional | Individual DD transaction |
| `4` | Direct Credit Input | Conditional | Individual DC transaction |
| `5` | Trailer Label | Yes | Count of records in the file |
| `6` | Contras | No | Contra entries for net settlement |
| `9` | User Trailer | Yes | Total volume/value cross-check |
| `A` | AUDDIS Instruction | No | New/amend/cancel mandate |
| `B` | ARUDD Return | No | Return of unpaid DD |

### Example: Record 3 (Direct Debit)

```
Position  Length  Field                Format    Example
1         1       Record Type          N         3
2-8       7       Volume Header No     N(7)      0000001
9-17      9       Destination Sort     N(9)      200415000  (sort + suffix)
18-26     9       Destination Account  N(9)      000123456
27-37     11      Amount (pence)       N(11)     0000037500  (£375.00)
38-52     15      Originator Sort + Ac N(15)     BARCGB2L12345
53-53     1       Transaction Type     AN        1  (== recurring DD)
54-67     14      Reference            AN(14)    INV-2026-001
68-79     12      Originating SU Code  AN(12)    SUCODE123456
80-80     1       Blank / filler       X         (space)
```

### Example: Record 4 (Direct Credit)

```
Position  Length  Field                Format    Example
1         1       Record Type          N         4
2-8       7       Volume Header No     N(7)      0000002
9-17      9       Destination Sort     N(9)      200415000
18-26     9       Destination Account  N(9)      000123456
27-37     11      Amount (pence)       N(11)     0000025000  (£250.00)
38-52     15      Originator Name/FI   AN(15)    PAYERBANK12345
53-66     14      Payroll/Reference    AN(14)     PAYROLL-MAY26
67-79     13      Originating SU Code  AN(13)    SUCODE1234567
80-80     1      Blank / filler        X         (space)
```

---

## Standard 18 Processing Flow

```
Client submits Standard 18 file (text/plain)
  → POST /v1/payments/bacs/submit
  → Parse file, validate record syntax (80 chars, valid types, hash checks)
  → Assign to current open cycle (input day)
  → Return submission receipt with {id, volume, value, status: "ACCEPTED"}

Day 1 (Input Day):
  22:30 cut-off → no more submissions accepted for this cycle
  → POST /v1/payments/bacs/cycle/close (or automatic at 22:30)
  → Cycle status → "PROCESSING"

Day 2 (Processing):
  → Internal netting: calculate net positions per SU
  → Validate mandates for DD submissions
  → Generate reject ARUDD files
  → Cycle status → "AWAITING_SETTLEMENT"

Day 3 (Settlement):
  → 07:00 — net debit/credit applied
  → Generate settlement reports
  → Cycle status → "SETTLED"
  → Reports available via GET /v1/payments/bacs/reports/{cycle-date}
```

---

## Database Tables (BACS-specific)

Beyond shared participant tables:

| Table | Purpose |
| :--- | :--- |
| `bacs_submissions` | File metadata (filename, size, hash, SU, volume, value, status, cycle) |
| `bacs_transactions` | Parsed Standard 18 records (type 3/4 items linked to submission) |
| `bacs_cycles` | 3-day cycle tracking (input_date, processing_date, settlement_date, status) |
| `bacs_mandates` | AUDDIS mandate records (SU, Payer ref, account details, status) |
| `bacs_returns` | ARUDD return records (original transaction ref, reason code) |
| `bacs_journal_entries` | Immutable audit trail |
| `bacs_reports` | Pre-generated end-of-cycle settlement reports per SU |

---

## Testing

| Package | File | Tests | Description |
| :--- | :--- | :--- | :--- |
| `pkg/standard18` | `parser_test.go` | 13 | Basic file parsing, AUDDIS, CRLF handling, validation, pence conversion, multiple records, line padding, zero values |

Run all tests:

```bash
go test ./pkg/... -v -count=1
```

---

## Directory Structure

```
bacs-service/
├── cmd/server/main.go            # Bootstrap (DB → server → HTTP listener)
├── internal/db/
│   ├── 01_init.sql               # Full schema: 10 tables, custom enums
│   └── 02_seed.sql               # 4 banks + open cycle
├── pkg/server/server.go          # 27 endpoints, JSON + text/plain dispatch, SSE streaming
├── pkg/events/events.go          # In-memory EventBus for SSE real-time notifications
├── pkg/ledger/service.go         # Batch settlement, netting, cycles, mandates, reports
├── pkg/standard18/
│   ├── parser.go                 # Standard 18 fixed-width parser (178 lines)
│   └── parser_test.go            # 13 tests
├── Dockerfile                    # Multi-stage, CGO_ENABLED=0
├── compose.yml                   # Production: db + app (port 8082)
├── compose-dev.yml               # Dev: db only
├── go.mod / go.sum
└── README.md
```
