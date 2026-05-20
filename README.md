# UK Payment Systems (UKPS)

A multi-service simulation of the UK interbank payment network — **CHAPS** (RTGS, high-value), **FPS** (near-real-time, low-value), and **BACS** (batch, 3-day settlement) — plus a **Central Bank** supervisory dashboard.

Each service is an independent Go binary with its own PostgreSQL ledger, deployable together via Docker Compose.

## Services

| Service | Scheme | Settlement | App Port | DB Port | DB Name |
| :--- | :--- | :--- | :--- | :--- | :--- |
| [CHAPS](./chaps-service/) | ISO 20022 | RTGS | `8420` | `5420` | `chaps_ledger` |
| [FPS](./fps-service/) | ISO 20022 + ISO 8583 | DNS | `8421` | `5421` | `fps_ledger` |
| [BACS](./bacs-service/) | Standard 18 | 3-day net | `8422` | `5422` | `bacs_ledger` |

## Quick Start

```bash
docker compose up -d
```

This starts all four services and their databases. Service READMEs contain detailed API documentation, message formats, and settlement logic.

## Architecture

```
bacs-service ─── Postgres 18 (bacs_ledger)
fps-service  ─── Postgres 18 (fps_ledger)
chaps-service ── Postgres 18 (chaps_ledger)
```

Each service is fully independent — separate binary, separate database, separate container. They share a common participant registry pattern conceptually but do not share infrastructure. See `AGENTS.md` for implementation conventions.
