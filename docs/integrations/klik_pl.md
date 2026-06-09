# Integracja KLIK → CHAPS (UKPS)

KLIK wysyła przelewy międzybankowe do systemu RTGS po wykonaniu nettingu.
`chaps-service` udostępnia endpoint `/v1/klik/chaps/settle`, który przyjmuje
format KLIK-a i mapuje go na wewnętrzne rozliczenie CHAPS.

## Jak to działa

```
KLIK                              chaps-service (UKPS)
 │                                       │
 │  POST /v1/klik/chaps/settle           │
 │  {session_id, transfer_id,            │
 │   from, to, amount, currency}         │
 │ ────────────────────────────────────► │
 │                                       │
 │  • walidacja: GBP, poprawna kwota     │
 │  • lookup banku po nazwie w DB        │
 │  • sprawdzenie godzin pracy CHAPS     │
 │  • wykonanie settlementu              │
 │                                       │
 │  {transfer_id, status,                │
 │   rtgs_reference, failure_reason}     │
 │ ◄──────────────────────────────────── │
```

Banki są identyfikowane po **nazwie** (`participant_profiles.name`), która jest
unikalna. KLIK musi używać dokładnie tych samych nazw, pod którymi banki są
zarejestrowane w CHAPS.

## Endpointy

| Metoda | Ścieżka | Opis |
|---|---|---|
| `GET` | `/v1/klik/chaps/healthz` | Health check |
| `POST` | `/v1/klik/chaps/settle` | Wykonanie przelewu |

## Health check

```bash
docker run --rm --network chaps_klik curlimages/curl \
  http://chaps-app:8080/v1/klik/chaps/healthz
```

```json
{"status":"ok","system":"CHAPS"}
```

## Przelew — bank nie istnieje w serwisie CHAPS

Gdy KLIK wyśle nazwę banku, która nie figuruje w `participant_profiles`,
zwracamy `FAILED` z informacją który bank jest nieznany.

```bash
docker run --rm --network chaps_klik curlimages/curl \
  -X POST http://chaps-app:8080/v1/klik/chaps/settle \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "test-001",
    "transfer_id": "klik-test-001",
    "system": "CHAPS",
    "from": "Alice Bank",
    "to": "HSBC",
    "amount": "100.00",
    "currency": "GBP"
  }'
```

```json
{"transfer_id":"klik-test-001","status":"FAILED","rtgs_reference":"","failure_reason":"unknown receiver bank: HSBC"}
```

## Przelew — sukces

Wszystkie banki znane, płynność wystarczająca, godziny pracy CHAPS — przelew
wykonany.

```bash
docker run --rm --network chaps_klik curlimages/curl \
  -X POST http://chaps-app:8080/v1/klik/chaps/settle \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "test-001",
    "transfer_id": "klik-test-001",
    "system": "CHAPS",
    "from": "Alice Bank",
    "to": "Barclays Bank",
    "amount": "100.00",
    "currency": "GBP"
  }'
```

```json
{"transfer_id":"klik-test-001","status":"SUCCESS","rtgs_reference":"CHAPS-KLIK-TEST-00"}
```

## Konfiguracja po stronie KLIK

Zmiany w `.env`:

```
CHAPS_URL=http://chaps-app:8080/v1/klik/chaps
```

Zmiany w `docker-compose.yml`:

- Kontener KLIK-a musi być na wspólnej sieci Docker z `chaps-app`: `chaps_klik`
- Nazwaną sieć `chaps_klik` należy dodać do serwisu `rtgs_mock`

```yaml
rtgs-mock:
    build: ./rtgs_mock
    ...
    networks:
      - klik_net
      - chaps_klik

networks:
  chaps_klik:
    name: chaps_klik
```

## Lista banków

Aktualne banki zarejestrowane w CHAPS (seed):

| Nazwa w DB (`name`) | BIC |
|---|---|
| Alice Bank | `SNDRUK22` |
| Barclays Bank | `BARCGB2L` |
| HSBC UK | `HSBCGB44` |
| Lloyds Bank | `LLOYGB21` |

KLIK musi używać tych samych nazw w polu `from` / `to`. Nowe banki można
dodać przez `POST /v1/participants/register` i są od razu dostępne dla KLIK.
