# Kody błędów — UKPS

Wszystkie serwisy UKPS zwracają błędy w ujednoliconym formacie JSON:

```json
{"error": "opis błędu"}
```

---

## Kody HTTP

| Kod | Znaczenie | Kategoria | Akcja klienta |
|---|---|---|---|
| 200 | OK | Sukces | Operacja zakończona |
| 201 | Created | Sukces | Zasób utworzony |
| 202 | Accepted | Sukces | Żądanie przyjęte (PDNG, RJCT lub przetwarzanie async) |
| 400 | Bad Request | Błąd klienta | Sprawdź payload, BIC, kwotę |
| 404 | Not Found | Błąd klienta | Zasób nie istnieje |
| 409 | Conflict | Błąd klienta | Konflikt stanu (np. anulowanie rozliczonej płatności) |
| 415 | Unsupported Media Type | Błąd klienta | Nieprawidłowy Content-Type |
| 500 | Internal Server Error | Błąd serwera | Spróbuj ponownie, zgłoś jeśli się powtarza |
| 503 | Service Unavailable | Błąd serwera | Poza godzinami operacyjnymi lub system niedostępny |

---

## Kody błędów specyficzne

### CHAPS

| Kod HTTP | Sytuacja |
|---|---|
| 200 | Płatność rozliczona (ACTC) |
| 202 (ACTC) | Płatność rozliczona ale zwrócona przez idempotencję |
| 202 (PDNG) | Brak płynności — zakolejkowane |
| 202 (RJCT) | Odrzucone: nieznany BIC, konto zablokowane, nieprawidłowy XML |
| 400 | Brak msg_id, nieprawidłowy BIC, kwota ≤ 0 |
| 404 | Transakcja nie istnieje |
| 409 | Próba anulowania SETTLED/QUEUED |
| 415 | Należy użyć `application/json` lub `application/xml` |
| 503 | Poza godzinami operacyjnymi (06:00-18:00) |

### FPS

| Kod HTTP | Sytuacja |
|---|---|
| 200 | SIP rozliczony (ACTC) |
| 201 | Forward-dated lub standing order utworzony |
| 202 (PDNG) | Brak płynności |
| 202 (RJCT) | Odrzucone |
| 400 | Brakujące pole, nieprawidłowy BIC |
| 404 | Transakcja lub alias nie istnieje |
| 409 | Recall nierozliczonej płatności |
| 415 | Należy użyć `application/json`, `application/xml` lub `application/octet-stream` |
| 503 | Poza godzinami operacyjnymi |

### BACS

| Kod HTTP | Sytuacja |
|---|---|
| 200 | Sukces |
| 201 | Zgłoszenie przyjęte, mandat utworzony |
| 202 | Plik w trakcie przetwarzania |
| 400 | Nieprawidłowy Standard 18, suma kontrolna |
| 404 | Zgłoszenie/cykl/mandat nie istnieje |
| 409 | Po cut-off (22:30) |
| 415 | Należy użyć `text/plain` lub `multipart/form-data` |
| 503 | Poza oknem input day |

---

## ISO 8583 — response codes (DE39)

| Kod | Opis |
|---|---|
| `000` | Approved |
| `051` | Insufficient funds |
| `057` | Not permitted |

---

## ISO 20022 — reason codes (pacs.002)

| Code | Znaczenie |
|---|---|
| `AC01` | Unknown account |
| `AC04` | Closed/blocked account |
| `XMLI` | XML/schema invalid |
| `PARSE-ERR` | XML parse error |
| `INVALID-FIELDS` | Missing or invalid fields |
| `SCHEMA-ERR` | XSD validation failed |
| `MSGID-TOO-LONG` | MsgId exceeds 35 characters |
| `INSU` | Insufficient liquidity |
