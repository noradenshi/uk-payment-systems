# BACS — Dokumentacja integracyjna

Dokument dla uczestników rozliczających się w systemie BACS — płatności batchowych w 3-dniowym cyklu settlement.

**Wersja:** 1.0
**Status:** Kompletny

---

## Spis treści

1. [Słownik domenowy](#słownik-domenowy)
2. [Obsługa błędów](#obsługa-błędów)
3. [API reference](#api-reference)
4. [Format Standard 18](#format-standard-18)
5. [Cykl 3-dniowy](#cykl-3-dniowy)
6. [AUDDIS — Direct Debit Mandates](#auddis)
7. [ARUDD — Returns i Rejects](#arudd)

---

## Słownik domenowy

| Termin | Definicja |
|---|---|
| **BACS** | Bankers Automated Clearing Services — brytyjski system rozliczeń batchowych |
| **SU** | Service User — podmiot zlecający Direct Debits lub Direct Credits (np. dostawca energii) |
| **DSU** | Destination Service User — podmiot otrzymujący płatność |
| **Standard 18** | Format rekordów o stałej szerokości (80 znaków), 18 typów rekordów |
| **AUDDIS** | Automated Direct Debit Instruction Service — zarządzanie mandatami DD |
| **ARUDD** | Automated Return of Unpaid Direct Debits — zwroty nieopłaconych DD |
| **BACSTEL-IP** | Protokół transmisji plików (FTP/HTTPS file upload) |
| **Input Day** | Dzień 1 — przyjmowanie plików przed cut-off |
| **Processing Day** | Dzień 2 — sortowanie, netting, walidacja mandatów |
| **Settlement Day** | Dzień 3 — debet/kredyt netto, raporty |
| **Volume** | Liczba transakcji w zgłoszeniu |
| **Value** | Łączna wartość pieniężna zgłoszenia lub cyklu |

---

## Obsługa błędów

| Kod HTTP | Kategoria | Kiedy występuje |
|---|---|---|
| 200 | Sukces | Operacja zakończona sukcesem |
| 201 | Utworzono | Zgłoszenie przyjęte, mandat utworzony |
| 202 | Zaakceptowano | Plik w trakcie przetwarzania |
| 400 | Błąd walidacji | Nieprawidłowy format Standard 18, suma kontrolna niezgodna |
| 404 | Nie znaleziono | Zgłoszenie/cykl/mandat nie istnieje |
| 409 | Konflikt | Próba wycofania po cut-off |
| 415 | Nieobsługiwany typ | Należy użyć `text/plain` lub `multipart/form-data` |
| 500 | Błąd wewnętrzny | Nieoczekiwany błąd serwera |
| 503 | Niedostępny | Poza godzinami operacyjnymi (po input cut-off) |

---

## API reference

**Bazowy URL:** `http://localhost:8082/v1`

### Zgłoszenia plików

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/bacs/submit` | Prześlij plik Standard 18 (`text/plain` lub `multipart/form-data`) |
| **GET** | `/payments/bacs/submit/{id}` | Status zgłoszenia, volume, value, liczba błędów |
| **GET** | `/payments/bacs/submit` | Lista zgłoszeń filtrowana po statusie, SU, dacie |
| **DELETE** | `/payments/bacs/submit/{id}` | Wycofaj zgłoszenie przed cut-off |

### Cykle

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/payments/bacs/cycle/current` | Bieżący cykl: daty input/processing/settlement, cut-off |
| **GET** | `/payments/bacs/cycle/{cycle-date}` | Konkretny cykl |
| **GET** | `/payments/bacs/cycle` | Historia cykli (30 dni) |
| **POST** | `/payments/bacs/cycle/close` | **Operator**: zamknij input day |
| **POST** | `/payments/bacs/cycle/process` | **Operator**: przesuń do AWAITING_SETTLEMENT |
| **POST** | `/payments/bacs/cycle/settle` | **Operator**: rozlicz pozycje netto |
| **GET** | `/payments/bacs/incoming/{bic}` | SSE — strumień `cycle.settled` |

### Mandaty AUDDIS

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/bacs/mandates` | Utwórz mandat |
| **GET** | `/payments/bacs/mandates/{ref}` | Szczegóły mandatu |
| **GET** | `/payments/bacs/mandates` | Lista mandatów SU/DSU |
| **PATCH** | `/payments/bacs/mandates/{ref}` | Zmiana mandatu |
| **DELETE** | `/payments/bacs/mandates/{ref}` | Anuluj mandat |
| **POST** | `/payments/bacs/mandates/{ref}/claim` | Zgłoś roszczenie mandatu |

### Zwroty i odrzucenia

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/payments/bacs/returns` | Lista ARUDD returns |
| **POST** | `/payments/bacs/returns` | Zgłoś return ARUDD |
| **GET** | `/payments/bacs/rejects` | Lista odrzuceń walidacyjnych |

### Raporty

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/payments/bacs/reports/{cycle-date}` | Raporty settlement za cykl |
| **GET** | `/payments/bacs/reports/{cycle-date}/su/{bic}` | Raport per SU |
| **GET** | `/payments/bacs/reports/{cycle-date}/summary` | Podsumowanie cyklu |
| **GET** | `/payments/bacs/reports/su/{bic}` | Historia raportów SU |

### Limity

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/payments/bacs/limits` | Limity BACS |
| **PATCH** | `/payments/bacs/limits/{bic}` | Limit uczestnika |

### System

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/system/schedule` | Daty kolejnych cykli |

---

## Format Standard 18

Standard 18 to format **stałej szerokości** — każdy rekord ma dokładnie **80 znaków** (z znakiem nowej linii).

### Typy rekordów

| Typ | Nazwa | Obowiązkowy | Cel |
|---|---|---|---|
| `1` | Volume Header | Tak | Nagłówek pliku: SU, DSU, data, wartość, wolumen |
| `2` | Output Spec | Tak | Sort code banku/docelowego DSU |
| `3` | Direct Debit Input | Warunkowo | Pojedyncza transakcja DD |
| `4` | Direct Credit Input | Warunkowo | Pojedyncza transakcja DC |
| `5` | Trailer Label | Tak | Liczba rekordów w pliku |
| `6` | Contras | Nie | Entries contra dla net settlement |
| `9` | User Trailer | Tak | Suma krzyżowa volume/value |
| `A` | AUDDIS Instruction | Nie | Instrukcja mandatu (nowy/zmiana/anuluj) |
| `B` | ARUDD Return | Nie | Zwrot nieopłaconego DD |

### Rekord 3 (Direct Debit)

```
Pozycja  Długość  Pole                Format    Przykład
1        1        Typ rekordu          N         3
2-8      7        Volume Header No     N(7)      0000001
9-17     9        Sort code docelowy   N(9)      200415000
18-26    9        Konto docelowe       N(9)      000123456
27-37    11       Kwota (pence)        N(11)     0000037500
38-52    15       Sort code nadawcy    N(15)     BARCGB2L12345
53-53    1        Typ transakcji       AN        1
54-67    14       Referencja           AN(14)    INV-2026-001
68-79    12       Kod SU nadawcy       AN(12)    SUCODE123456
80-80    1        Wypełniacz           X         (spacja)
```

### Rekord 4 (Direct Credit)

```
Pozycja  Długość  Pole                Format    Przykład
1        1        Typ rekordu          N         4
2-8      7        Volume Header No     N(7)      0000002
9-17     9        Sort code docelowy   N(9)      200415000
18-26    9        Konto docelowe       N(9)      000123456
27-37    11       Kwota (pence)        N(11)     0000025000
38-52    15       Nazwa nadawcy        AN(15)    PAYERBANK12345
53-66    14       Referencja           AN(14)    PAYROLL-MAY26
67-79    13       Kod SU nadawcy       AN(13)    SUCODE1234567
80-80    1        Wypełniacz           X         (spacja)
```

### Konwersja kwot

Wszystkie kwoty w plikach Standard 18 przechowywane są w **pensach** (liczby całkowite).
Parser dzieli przez 100.0 w celu uzyskania wartości w GBP (`float64`).

---

## Cykl 3-dniowy

### Schemat

```
Dzień 1 (Input Day):
  22:30 cut-off → brak nowych zgłoszeń
  → POST /cycle/close (ręczne lub scheduler)
  → Cykl: PROCESSING

Dzień 2 (Processing):
  → Netting: pozycje netto per SU
  → Walidacja mandatów DD
  → Generowanie ARUDD rejects
  → Cykl: AWAITING_SETTLEMENT

Dzień 3 (Settlement):
  07:00 — debet/kredyt netto
  → Raporty settlement dostępne
  → Cykl: SETTLED
```

### Automatyzacja (scheduler)

Scheduler odczytuje konfigurację z `config/sessions.json`:
- `processing_duration_minutes` — czas od OPEN do PROCESSING
- `settlement_duration_minutes` — dodatkowy czas od PROCESSING do SETTLED
- W trybie demo: wartości capped do `demo_session_minutes`

### SSE

`GET /v1/payments/bacs/incoming/{bic}` emituje `cycle.settled`:
```
data: {"type":"cycle.settled","data":{"cycle_date":"2026-06-03","status":"SETTLED"}}
```

---

## AUDDIS

AUDDIS (Automated Direct Debit Instruction Service) umożliwia elektroniczne zarządzanie mandatami DD.

### Stany mandatu

`PENDING → ACTIVE | REJECTED → CANCELLED`

### Operacje

- **Utworzenie** — bank wierzyciela rejestruje nowy mandat przez API
- **Zmiana** — aktualizacja kwoty, częstotliwości, dat
- **Anulowanie** — zamknięcie mandatu
- **Roszczenie** — zgłoszenie roszczenia wobec konkretnego konta

---

## ARUDD

ARUDD (Automated Return of Unpaid Direct Debits) obsługuje zwroty nieopłaconych transakcji DD.

### Powody zwrotu

| Kod | Opis |
|---|---|
| `0` | Referencja nie istnieje |
| `1` | Konto zamknięte |
| `2` | Konto zablokowane |
| `3` | Brak środków |
| `4` | Mandat anulowany |
| `5` | Mandat wygasły |
| `9` | Inne |

---

## Uczestnicy (seed)

| BIC | Nazwa | Rola |
|---|---|---|
| `BARCGB2L` | Barclays Bank | SU/DSU |
| `HSBCGB44` | HSBC UK | SU/DSU |
| `LLOYGB21` | Lloyds Bank | SU/DSU |
| `SNDRUK22` | Alice Bank | SU/DSU |
