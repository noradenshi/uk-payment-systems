# UK Payment Systems (UKPS)

> Symulacja brytyjskiej infrastruktury rozliczeń międzybankowych — **CHAPS** (RTGS, wysokowartościowe), **FPS** (płatności natychmiastowe), **BACS** (batch, 3-dniowy cykl) — plus panel operatorski **Central Bank**.

**Projekt akademicki:** Aplikacje Biznesowe

---

## Spis treści

1. [O projekcie](#o-projekcie)
2. [Status](#status)
3. [Architektura](#architektura)
4. [Stack technologiczny](#stack-technologiczny)
5. [Quick Start](#quick-start)
6. [Dokumentacja](#dokumentacja)
7. [Porty](#porty)
8. [Gridlock Resolution](#gridlock-resolution)

---

## O projekcie

UKPS pełni rolę symulatora brytyjskiego systemu płatności międzybankowych. Zapewnia trzy serwisy rozliczeniowe:

1. **CHAPS** — Real-Time Gross Settlement dla płatności wysokowartościowych (ISO 20022). Każda płatność rozliczana indywidualnie i nieodwołalnie.
2. **FPS** — Faster Payments Service (ISO 20022 + ISO 8583). Płatności niskokwotowe w cyklach DNS z opcją SIP.
3. **BACS** — Bankers Automated Clearing Services (Standard 18). Batch processing w 3-dniowym cyklu net settlement.

System obsługuje wyłącznie strefę **UK (GBP)**. Wszystkie serwisy są w pełni niezależne — osobne binarne Go, osobne bazy PostgreSQL.

---

## Status

| Moduł | Status | Dokumentacja |
|---|---|---|
| CHAPS — backend Go | ✅ Kompletny | [README](./chaps-service/README.md) · [Integracja](./docs/chaps/integration/INFO.md) |
| CHAPS — web GUI (React) | ✅ Kompletny | |
| FPS — backend Go | ✅ Kompletny | [README](./fps-service/README.md) · [Integracja](./docs/fps/integration/INFO.md) |
| FPS — ISO 8583 TCP socket | ✅ Kompletny | |
| FPS — web GUI (React) | ✅ Kompletny | |
| BACS — backend Go | ✅ Kompletny | [README](./bacs-service/README.md) · [Integracja](./docs/bacs/integration/INFO.md) |
| BACS — Standard 18 parser | ✅ Kompletny | |
| Gridlock resolution | ✅ Kompletny | |
| SSE real-time events | ✅ Kompletny | |
| Scheduler (config-driven) | ✅ Kompletny | |
| Central Bank dashboard | 🟡 W trakcie | |

---

## Architektura

```
┌──────────────────────┐    ┌──────────────────────┐    ┌──────────────────────┐
│    bacs-service      │    │    fps-service        │    │   chaps-service      │
│    (Standard 18)     │    │ (ISO 20022 + ISO 8583)│    │   (ISO 20022)        │
│    Batch / 3-day     │    │   Near-real-time      │    │   RTGS / High-value  │
│    Port 8082         │    │   Port 8081           │    │   Port 8080          │
└────────┬─────────────┘    └────────┬──────────────┘    └────────┬──────────────┘
         │                           │                           │
         └───────────────────────────┼───────────────────────────┘
                                     │
                     ┌───────────────▼────────────────┐
                     │        PostgreSQL 18            │
                     │  (3 osobne bazy danych)         │
                     └────────────────────────────────┘
```

Każdy serwis jest w pełni niezależny — osobny binary, osobna baza, osobny kontener. Współdzielą koncepcyjnie rejestr uczestników, ale nie współdzielą infrastruktury.

---

## Stack technologiczny

| Komponent | Technologia |
|---|---|
| Backend | Go 1.25+ (standard library `net/http`, `pgx/v5`) |
| Bazy danych | PostgreSQL 18 (natywne `uuidv7()`) |
| Format wiadomości | ISO 20022 XML, ISO 8583 binary, Standard 18 fixed-width |
| Walidacja XSD | libxml2 przez CGO (CHAPS + FPS) |
| Web GUI | React 18 + Vite 5 + TypeScript 5 |
| Real-time | SSE (EventBus in-memory) |
| Konteneryzacja | Docker + Docker Compose |
| Scheduler | `config/sessions.json` + `time.Ticker` |

---

## Quick Start

```bash
# Development (tylko bazy danych)
docker compose -f compose-dev.yml up -d

# Produkcja (wszystkie serwisy)
docker compose up -d --build
```

Po uruchomieniu:

| URL | Co to |
|---|---|
| `http://localhost:8080` | CHAPS API + GUI |
| `http://localhost:8081` | FPS API + GUI |
| `http://localhost:8082` | BACS API |
| `http://localhost:7421` | FPS — ISO 8583 TCP socket |

---

## Dokumentacja

### Integracja (dla banków / uczestników)

| Serwis | Dokumentacja | Format |
|---|---|---|
| **CHAPS** | [docs/chaps/integration/INFO.md](./docs/chaps/integration/INFO.md) | ISO 20022 |
| **FPS** | [docs/fps/integration/INFO.md](./docs/fps/integration/INFO.md) | ISO 20022 + ISO 8583 |
| **BACS** | [docs/bacs/integration/INFO.md](./docs/bacs/integration/INFO.md) | Standard 18 |

### System

| Dokument | Opis |
|---|---|
| [docs/architecture.md](./docs/architecture.md) | Architektura systemu, wspólne wzorce |
| [docs/error-codes.md](./docs/error-codes.md) | Kody błędów (wszystkie serwisy) |
| [docs/deployment.md](./docs/deployment.md) | Deployment guide (dev/prod) |
| [AGENTS.md](./AGENTS.md) | Konwencje implementacyjne dla AI |

---

## Porty

| Serwis | Port HTTP | Port DB | Inne |
|---|---|---|---|
| CHAPS | 8080 | 5432 | — |
| FPS | 8081 | 5433 | TCP 7421 (ISO 8583) |
| BACS | 8082 | 5434 | — |

Połączenia DB używają `127.0.0.1` zamiast `localhost` aby uniknąć niejednoznaczności socketu Unix.

---

## Gridlock Resolution

W systemach RTGS (CHAPS) i near-real-time (FPS) płatności mogą zostać **zakolejkowane** gdy nadawcy brakuje płynności. Jeśli wielu uczestników jednocześnie oczekuje na środki od siebie nawzajem, system wchodzi w **gridlock** — cykliczną zależność, gdzie żadna zakolejkowana płatność nie może się rozliczyć indywidualnie.

Algorytm rozwiązywania gridlocka:
1. Skanuje wszystkie QUEUED transakcje w kolejności utworzenia
2. Dla każdej sprawdza czy płynność nadawcy (z uwzględnieniem overdraftu) pokrywa kwotę
3. Rozlicza kwalifikujące się płatności: debet/kredyt na kontach
4. Powtarza aż do braku dalszego postępu

Uruchamiany **automatycznie** po każdym zasileniu płynności oraz **ręcznie** przez `POST /v1/payments/{chaps,fps}/gridlock/resolve`.

BACS (batch net settlement) nie jest dotknięty gridlockiem — rozlicza na zasadzie sald netto.

---

**Przedmiot:** Aplikacje Biznesowe  
**Prowadzący:** mgr inż. Marcin Mrukowicz  
**Rok akademicki:** 2025/2026
