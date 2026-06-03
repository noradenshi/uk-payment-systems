# Deployment guide — UKPS

UKPS nie rozdziela osobnych plików dla dev i produkcji — topologia jest jedna (`compose.yml`).
Tryb pracy (demo / production) wybiera się przez pole `mode` w `config/sessions.json` każdego serwisu.

| Tryb | Zachowanie |
|---|---|
| `demo` | Przyśpieszony tick (co `demo_session_minutes` s), skrócone czasy cykli |
| `production` | Tick co 60s, rzeczywiste czasy (np. 1440 min dla BACS, wall-clock settlement_times dla FPS) |

---

## Topologia (produkcja)

```
┌───────────────────────────────────────────────────────────────┐
│                      docker compose                           │
│                                                               │
│  ┌────────────┐   ┌────────────┐   ┌────────────┐           │
│  │ chaps-db   │   │ fps-db     │   │ bacs-db    │           │
│  │ postgres   │   │ postgres   │   │ postgres   │           │
│  │ :5420      │   │ :5421      │   │ :5422      │           │
│  └─────┬──────┘   └─────┬──────┘   └─────┬──────┘           │
│        │                │                │                   │
│  ┌─────▼──────┐   ┌─────▼──────┐   ┌─────▼──────┐           │
│  │ chaps-app  │   │ fps-app    │   │ bacs-app   │           │
│  │ :8420      │   │ :8421      │   │ :8422      │           │
│  └────────────┘   └─────┬──────┘   └────────────┘           │
│                         │                                    │
│                  ┌──────▼──────┐                             │
│                  │ fps ISO 8583 │                             │
│                  │ :7421       │                             │
│                  └─────────────┘                             │
└───────────────────────────────────────────────────────────────┘
```

### Porty

| Serwis | Port kontenera | Port hosta |
|---|---|---|
| CHAPS APP | 8080 | 8420 |
| FPS APP | 8081 | 8421 |
| FPS ISO 8583 | 7421 | 7421 |
| BACS APP | 8082 | 8422 |
| CHAPS DB | 5432 | 5420 |
| FPS DB | 5432 | 5421 |
| BACS DB | 5432 | 5422 |

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

---

## Tryb demo

W `config/sessions.json` ustaw `"mode": "demo"` i `"demo_session_minutes": 15` (lub mniej).

W trybie demo:
- Tick schedulera = `min(demo_session_minutes, 60)` s
- Czasy cykli BACS capped do `demo_session_minutes`
- Cykle DNS FPS zamykane co `demo_session_minutes`

---

## Wymagania systemowe

| Komponent | Wersja |
|---|---|
| Go | 1.26 (obraz `golang:1.26-alpine`) |
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

## Testowanie

Testy jednostkowe znajdują się w podfolderach każdego serwisu, obok testowanego kodu:

| Serwis | Pliki testowe |
|---|---|
| CHAPS | `chaps-service/pkg/iso20022/serialization_test.go` |
|       | `chaps-service/pkg/ledger/service_test.go` |
|       | `chaps-service/pkg/server/server_test.go` |
| FPS | `fps-service/pkg/iso20022/serialization_test.go` |
|     | `fps-service/pkg/iso8583/message_test.go` |
|     | `fps-service/pkg/server/server_test.go` |
|     | `fps-service/pkg/validator/validator_test.go` |
| BACS | `bacs-service/pkg/standard18/parser_test.go` |
|      | `bacs-service/pkg/server/server_test.go` |

Uruchomienie wszystkich testów w serwisie:

```bash
cd {service} && go test ./...
```

Integracyjny smoke test (`test/integration_test.sh`) uruchamia bazy przez `compose-dev.yml`, buduje serwisy i testuje HTTP + SSE.
