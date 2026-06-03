# Architektura systemu UKPS

UKPS (UK Payment Systems) to symulacja brytyjskiej infrastruktury rozliczeń międzybankowych — trzy niezależne serwisy Go + jeden panel operatorski Central Bank.

---

## Diagram

```
┌──────────────────────┐    ┌──────────────────────┐    ┌──────────────────────┐
│    bacs-service      │    │    fps-service        │    │   chaps-service      │
│    (Standard 18)     │    │ (ISO 20022 + ISO 8583)│    │   (ISO 20022)        │
│    Batch / 3-day     │    │   Near-real-time      │    │   RTGS / High-value  │
│    Port 8082         │    │   Port 8081           │    │   Port 8080          │
└────────┬─────────────┘    └────────┬──────────────┘    └────────┬──────────────┘
         │                           │                           │
         ▼                           ▼                           ▼
┌──────────────┐          ┌──────────────┐          ┌──────────────┐
│ Postgres 18  │          │ Postgres 18  │          │ Postgres 18  │
│ bacs_ledger  │          │ fps_ledger   │          │ chaps_ledger │
│ :5434        │          │ :5433        │          │ :5432        │
└──────────────┘          └──────────────┘          └──────────────┘
```

Każdy serwis to w pełni niezależny binarny Go z własną bazą PostgreSQL — osobne bazy, osobne tabele, brak współdzielenia infrastruktury. Wszystkie serwisy stosują ten sam wzorzec schematu (normalizacja uczestników na 3 tabele), ale każda baza zawiera własne kopie danych.

---

## Opis serwisów

| Serwis | Mechanizm rozliczeń | Typ płatności | Format komunikatów |
|---|---|---|---|
| **CHAPS** | RTGS (Real-Time Gross Settlement) | Wysokowartościowe, natychmiastowe | ISO 20022 (pacs.008/pacs.002) |
| **FPS** | DNS (Deferred Net Settlement) + SIP | Niskokwotowe, zbliżone do rzeczywistego | ISO 20022, ISO 8583 binary |
| **BACS** | Batch 3-day net settlement | Niskokwotowe, batch | Standard 18 (fixed-width) |

---

## Wspólne wzorce

### Normalizacja baz danych

Każdy serwis dzieli uczestników na 3 tabele (różne częstotliwości aktualizacji):

| Tabela | Cel |
|---|---|
| `participant_profiles` | Dane statyczne (BIC, nazwa, waluta, sort code) |
| `participant_liquidity` | Wysokoczęstotliwościowe salda |
| `participant_statuses` | Status operacyjny, blokady, overdraft |

Oraz 2 tabele transakcyjne (osobne per baza):

| Tabela | Cel |
|---|---|
| `transactions` / `{scheme}_transactions` | Rekordy płatności, UUID v7 PK |
| `journal_entries` / `{scheme}_journal_entries` | Niezmienny audyt z `pg_notify` trigger |

### Idempotencja

```sql
INSERT INTO transactions (msg_id, ...) VALUES (...)
ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
RETURNING id, status
```

Jeśli status = `SETTLED` → zwróć cache ACTC (bez podwójnego rozliczenia).

### Gridlock resolution

Automatyczny algorytm rozwiązywania zakleszczeń:
1. Pobranie QUEUED transakcji w kolejności utworzenia
2. Dla każdej: sprawdzenie płynności nadawcy (z overdraftem)
3. Rozliczenie kwalifikujących się
4. Powtarzanie aż do braku postępu

Uruchamiany automatycznie po top-upie i ręcznie przez `POST /v1/payments/{scheme}/gridlock/resolve`.

### SSE (Server-Sent Events)

In-memory EventBus publikuje zdarzenia w czasie rzeczywistym:

| Serwis | Endpoint | Zdarzenie |
|---|---|---|
| CHAPS | `GET /v1/payments/chaps/incoming/{bic}` | `payment.received` |
| FPS | `GET /v1/payments/fps/incoming/{bic}` | `payment.received` |
| BACS | `GET /v1/payments/bacs/incoming/{bic}` | `cycle.settled` |

### Harmonogram (scheduler)

Każdy serwis uruchamia scheduler oparty o `config/sessions.json`:
- Tick: `min(demo_session_minutes, 60)` sekund w demo, 60s w produkcji
- CHAPS: wymusza blokady płynnościowe (2h breach → SUSPENDED)
- FPS: wykonuje forward-dated, standing orders, zamyka cykle DNS
- BACS: przesuwa cykle OPEN → PROCESSING → AWAITING_SETTLEMENT → SETTLED

---

## Porty

| Serwis | HTTP | DB | Inne |
|---|---|---|---|
| CHAPS | 8080 | 5432 | — |
| FPS | 8081 | 5433 | TCP 7421 (ISO 8583) |
| BACS | 8082 | 5434 | — |
