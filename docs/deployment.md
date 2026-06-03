# Deployment guide — UKPS

Tryb pracy (demo/production) sterowany jest przez `demo_session_minutes` w `config/sessions.json` — **nie** przez osobne pliki compose ani zmienne środowiskowe deploymentu. Pole `mode` w configu istnieje wyłącznie jako dokumentacja; kod Go sprawdza tylko `demo_session_minutes > 0`.

---

## Topologia

```
┌────────────────────────────────────────────────────────────────────┐
│                        docker-compose                              │
│                                                                    │
│  ┌────────────┐   ┌────────────┐   ┌────────────┐                │
│  │ chaps-db   │   │ fps-db     │   │ bacs-db    │                │
│  │ postgres   │   │ postgres   │   │ postgres   │                │
│  │ :5420      │   │ :5421      │   │ :5422      │                │
│  └─────┬──────┘   └─────┬──────┘   └─────┬──────┘                │
│        │                │                │                        │
│  ┌─────▼──────┐   ┌─────▼──────┐   ┌─────▼──────┐                │
│  │ chaps-app  │   │ fps-app    │   │ bacs-app   │                │
│  │ :8420      │   │ :8421 +    │   │ :8422      │                │
│  │ Go + React │   │ :7421      │   │ Go         │                │
│  └────────────┘   │ Go + React │   └────────────┘                │
│                   └────────────┘                                  │
└────────────────────────────────────────────────────────────────────┘
```

---

## Uruchomienie

```bash
# Uruchom wszystkie serwisy + bazy (z rebuildem)
docker compose up --build -d
```

Po `up --build -d`:

| URL | Co to |
|---|---|
| `http://localhost:8420` | CHAPS API + GUI |
| `http://localhost:8421` | FPS API + GUI |
| `http://localhost:8422` | BACS API |
| `:7421` | FPS — ISO 8583 TCP socket |

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

### Zatrzymanie

```bash
# Zatrzymaj bez utraty danych
docker compose down

# Pełny reset (usuwa volumes)
docker compose down -v
```

---

## Tryb demo a production

Ustawienie `demo_session_minutes` w `config/sessions.json` zmienia jedynie **szybkość** symulacji — nie wpływa na architekturę, porty, ani topologię. Godziny operacyjne (`opening_time`, `closing_time`, `interbank_cutoff`, `input_cutoff`) są zawsze egzekwowane względem czasu wall-clock, niezależnie od trybu.

| demo_session_minutes | Tick schedulera | FPS DNS | BACS cykl |
|---|---|---|---|
| `> 0` (demo) | `min(demo, 60)` s | Cykl zamykany co `demo_session_minutes` | Czas trwania capped do `demo_session_minutes` |
| `0` / brak (production) | 60 s | Zamknięcie o wall-clock `settlement_times` | Pełne 1440 min na fazę |

### Przykład

```json
"fps": {
    "mode": "demo",
    "demo_session_minutes": 15,
    "settlement_times": ["03:00", "09:00", "12:00", "15:00", "18:00", "21:00"]
}
```

- Tryb **demo** (`demo_session_minutes: 15`): tick co 15s, DNS zamykany co 15 min, godziny operacyjne 00:00-23:59 wall-clock.
- Tryb **production** (`demo_session_minutes: 0`): tick co 60s, DNS zamykany o 03:00/09:00/12:00/15:00/18:00/21:00 wall-clock.

---

## Wymagania systemowe

| Komponent | Wersja |
|---|---|
| Go | 1.26 (obraz `golang:1.26-alpine`) |
| Docker | 24+ |
| Docker Compose | v2 |
| PostgreSQL | 18 (z `uuidv7()`) |

### Uwagi

- **CHAPS + FPS wymagają CGO** — libxml2 do walidacji XSD. Build statyczny w Alpine.
- **BACS** — CGO-free, prostszy build.
- **Postgres 18** — wymagana funkcja `uuidv7()`.
- **DATABASE_URL** używa `127.0.0.1` zamiast `localhost` aby uniknąć niejednoznaczności socketu Unix.

---

## Testowanie

Testy jednostkowe w podfolderach każdego serwisu:

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

```bash
cd {service} && go test ./...
```

Integracyjny smoke test (`test/integration_test.sh`) — uruchamia bazy przez `compose-dev.yml`, buduje serwisy na hoście, testuje HTTP.
