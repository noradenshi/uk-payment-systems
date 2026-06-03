# Deployment guide — UKPS

UKPS wspiera dwie topologie uruchomienia:

| Tryb | Kiedy używać | Komenda |
|---|---|---|
| **Development** | Praca lokalna, tylko bazy danych | `docker compose -f compose-dev.yml up -d` |
| **Produkcja** | Wszystkie serwisy w kontenerach | `docker compose up -d` |

---

## Topologia

```
┌────────────────────────────────────────────────────────────────────┐
│                        docker-compose                              │
│                                                                    │
│  ┌────────────┐   ┌────────────┐   ┌────────────┐                │
│  │ chaps-db   │   │ fps-db     │   │ bacs-db    │                │
│  │ postgres   │   │ postgres   │   │ postgres   │                │
│  │ :5432      │   │ :5433      │   │ :5434      │                │
│  └─────┬──────┘   └─────┬──────┘   └─────┬──────┘                │
│        │                │                │                        │
│  ┌─────▼──────┐   ┌─────▼──────┐   ┌─────▼──────┐                │
│  │ chaps-app  │   │ fps-app    │   │ bacs-app   │                │
│  │ :8080      │   │ :8081      │   │ :8082      │                │
│  │ Go + React │   │ Go + React │   │ Go         │                │
│  └────────────┘   └────────────┘   └────────────┘                │
│                                                                    │
│  ┌────────────────────────────────────────────────────────────┐   │
│  │                    central-bank-service                     │   │
│  │                    :8083 (panel operatorski)                │   │
│  └────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────┘
```

---

## Development (praca lokalna)

Uruchamia tylko bazy danych — serwisy Go działają na hoście z hot-reload:

```bash
# 1. Uruchom bazy
docker compose -f compose-dev.yml up -d

# 2. Uruchom serwis (w osobnym terminalu)
cd chaps-service && go run ./cmd/server/main.go

# 3. Dla FPS lub BACS — analogicznie
cd fps-service && go run ./cmd/server/main.go
cd bacs-service && go run ./cmd/server/main.go

# 4. (Opcjonalnie) Panel Central Bank
cd central-bank-service && go run ./cmd/server/main.go
```

### Zmienne środowiskowe

Każdy serwis ma domyślne wartości w `main.go` — można nadpisać przez zmienne środowiskowe:

| Zmienna | Serwis | Domyślnie |
|---|---|---|
| `DATABASE_URL` | Wszystkie | `postgres://{user}:{pass}@127.0.0.1:{port}/{db}?sslmode=disable` |
| `ISO8583_PORT` | FPS | `:7421` |

---

## Produkcja (Docker Compose)

```bash
# Uruchom wszystkie serwisy
docker compose up -d --build

# Logi
docker compose logs -f

# Zatrzymaj
docker compose down
```

### Porty

| Serwis | Port kontenera | Port hosta |
|---|---|---|
| CHAPS APP | 8080 | 8080 |
| FPS APP | 8081 | 8081 |
| BACS APP | 8082 | 8082 |
| CHAPS DB | 5432 | 5432 |
| FPS DB | 5433 | 5433 |
| BACS DB | 5434 | 5434 |

---

## Wymagania systemowe

| Komponent | Wersja |
|---|---|
| Go | 1.25+ |
| Docker | 24+ |
| Docker Compose | v2 |
| PostgreSQL | 18 (z `uuidv7()`) |

### Uwagi

- **CHAPS wymaga CGO** — libxml2 do walidacji XSD. Build statyczny w Alpine.
- **FPS wymaga CGO** — identycznie jak CHAPS (libxml2). Oba serwisy używają tego samego image patternu.
- **BACS** — CGO-free, prostszy build.
- **Postgres 18** — używana jest funkcja `uuidv7()` niedostępna w starszych wersjach.
- **DATABASE_URL** używa `127.0.0.1` zamiast `localhost` aby uniknąć niejednoznaczności socketu Unix.

---

## Testowanie integracyjne

```bash
# Smoke test — uruchamia bazy, buduje serwisy, testuje HTTP
./test/integration_test.sh
```

Test sprawdza:
- Rejestrację uczestników
- Cykl życia płatności (każdy serwis)
- Gridlock resolution
- SSE events
