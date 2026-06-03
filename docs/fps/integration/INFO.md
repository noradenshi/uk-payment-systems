# FPS — Dokumentacja integracyjna

Dokument dla uczestników rozliczających się w systemie FPS — płatności niskokwotowych w czasie zbliżonym do rzeczywistego.

**Wersja:** 1.0
**Status:** Kompletny

---

## Spis treści

1. [Słownik domenowy](#słownik-domenowy)
2. [Obsługa błędów](#obsługa-błędów)
3. [API reference](#api-reference)
4. [Format ISO 20022](#format-iso-20022)
5. [Format ISO 8583](#format-iso-8583)
6. [Settlement DNS](#settlement-dns)
7. [Gridlock resolution](#gridlock-resolution)
8. [Gniazdo TCP ISO 8583](#gniazdo-tcp-iso-8583)

---

## Słownik domenowy

| Termin | Definicja |
|---|---|
| **FPS** | Faster Payments Service — brytyjski system płatności natychmiastowych |
| **SIP** | Single Immediate Payment — pojedyncza płatność rozliczana natychmiast (jeśli płynność pozwala) |
| **DNS** | Deferred Net Settlement — rozliczanie pozycji netto w cyklach w ciągu dnia |
| **Uczestnik bezpośredni** | Bank rozliczeniowy w ramach FPS, posiadający konto prefundowane |
| **Uczestnik pośredni** | Bank sponsoringowany przez uczestnika bezpośredniego |
| **Forward Dated** | Płatność zaplanowana na przyszłą datę |
| **Standing Order** | Zlecenie stałe (dzienne/tygodniowe/miesięczne) |
| **Bulk Payment** | Plik wielu płatności, netowanych razem |
| **ISO 8583** | Binarny format komunikatów finansowych (MTI 0200/0210) |
| **pacs.008** | ISO 20022 komunikat płatniczy |
| **pacs.002** | ISO 20022 komunikat statusu |
| **Prefunding** | Środki wpłacone z góry na konto rozliczeniowe FPS |
| **Gridlock** | Zakleszczenie — cykliczna zależność zakolejkowanych SIP |

---

## Obsługa błędów

| Kod HTTP | Kategoria | Kiedy występuje |
|---|---|---|
| 200 | Sukces | Płatność rozliczona (ACTC) |
| 201 | Utworzono | Forward-dated lub standing order utworzony |
| 202 | Zaakceptowano | Płatność zakolejkowana lub odrzucona |
| 400 | Błąd walidacji | Brakujące pole, nieprawidłowy BIC, kwota ≤ 0 |
| 404 | Nie znaleziono | Transakcja lub alias nie istnieje |
| 409 | Konflikt | Próba anulowania już rozliczonej płatności |
| 415 | Nieobsługiwany typ | Należy użyć `application/json`, `application/xml` lub `application/octet-stream` |
| 500 | Błąd wewnętrzny | Nieoczekiwany błąd serwera |
| 503 | Niedostępny | Poza godzinami operacyjnymi |

---

## API reference

**Bazowy URL:** `http://localhost:8081/v1`

### Płatności SIP

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/fps` | Zainicjuj SIP. Akceptuje ISO 20022 XML, ISO 8583 binary lub JSON |
| **POST** | `/payments/fps/validate` | Walidacja sucha |
| **GET** | `/payments/fps/{id}` | Status płatności, szczegóły, audyt |
| **GET** | `/payments/fps` | Lista/filtrowanie płatności |
| **DELETE** | `/payments/fps/{id}` | Recall płatności — tylko jeśli nierozliczona |
| **POST** | `/payments/fps/gridlock/resolve` | Rozwiąż gridlock |

### Forward Dated

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/fps/forward-dated` | Zaplanuj płatność przyszłą |
| **GET** | `/payments/fps/forward-dated` | Lista zaplanowanych płatności |
| **DELETE** | `/payments/fps/forward-dated/{id}` | Anuluj przed wykonaniem |

### Standing Orders

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/fps/standing-orders` | Utwórz zlecenie stałe |
| **GET** | `/payments/fps/standing-orders` | Lista zleceń stałych |
| **GET** | `/payments/fps/standing-orders/{id}` | Szczegóły i historia wykonania |
| **PATCH** | `/payments/fps/standing-orders/{id}` | Zmiana kwoty/częstotliwości/dat |
| **DELETE** | `/payments/fps/standing-orders/{id}` | Anuluj zlecenie |

### Bulk

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/fps/bulk` | Prześlij plik wielu płatności |
| **GET** | `/payments/fps/bulk/{id}` | Status i podgląd pozycji |
| **GET** | `/payments/fps/bulk` | Lista przesłanych plików |

### Uczestnicy

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/participants` | Lista uczestników |
| **POST** | `/participants/register` | Rejestracja |
| **PATCH** | `/participants/{bic}/status` | Zmiana statusu |
| **POST** | `/participants/{bic}/block` | Blokada awaryjna |
| **GET** | `/participants/{bic}/block` | Szczegóły blokady |
| **DELETE** | `/participants/{bic}/block` | Odblokowanie |
| **GET** | `/participants/{bic}/positions` | Pozycja (saldo prefundowane + DNS netto) |

### Rozliczenia i płynność

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/settlement/dns/cycle` | Bieżący cykl DNS |
| **POST** | `/settlement/dns/close` | Zamknij cykl DNS — oblicz pozycje netto, rozlicz |
| **GET** | `/settlement/dns/history` | Historia cykli DNS |
| **POST** | `/liquidity/top-up` | Symulacja prefundowania |
| **GET** | `/liquidity/prefunded/{bic}` | Saldo prefundowane uczestnika |

### Limity

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/payments/fps/limits` | Limity FPS |
| **PATCH** | `/payments/fps/limits/{bic}` | Aktualizacja limitu uczestnika |

### ISO 8583

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/fps/iso8583` | Przyjmuje binarny ISO 8583, zwraca 0210 |
| **GET** | `/payments/fps/iso8583/decode` | Dekoduje wiadomość ISO 8583 do JSON |

### System

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/system/schedule` | Harmonogram operacyjny |
| **GET** | `/payments/fps/incoming/{bic}` | SSE — strumień zdarzeń |

---

## Format ISO 20022

FPS akceptuje XML z kopertą `BizMsg` (identyczną jak w CHAPS):

```xml
<BizMsg>
  <AppHdr>...</AppHdr>
  <Document>
    <FIToFICstmrCdtTrf>
      <GrpHdr>
        <MsgId>FPS-20260603-001</MsgId>
      </GrpHdr>
      <CdtTrfTxInf>
        <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
        <IntrBkSttlmAmt Ccy="GBP">500.00</IntrBkSttlmAmt>
        <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
        <CdtrAgt><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></CdtrAgt>
      </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
  </Document>
</BizMsg>
```

Walidacja XSD przez libxml2 (chaps_wrapper.xsd), następnie parsowanie przez `xml.Unmarshal`.

Odpowiedź: `pacs.002.001.16` z statusem `ACTC`, `PDNG` lub `RJCT`.

---

## Format ISO 8583

### Obsługiwane MTI

| MTI | Nazwa | Kierunek |
|---|---|---|
| 0200 | Financial Transaction Request | Klient → FPS |
| 0210 | Financial Transaction Response | FPS → Klient |
| 0400 | Reversal Request | Klient → FPS |
| 0410 | Reversal Response | FPS → Klient |
| 0800 | Network Management Request | Klient → FPS |
| 0810 | Network Management Response | FPS → Klient |

### DE (Data Elements)

| DE | Nazwa | Format | Przykład |
|---|---|---|---|
| 2 | PAN / Account Number | LLVAR N..19 | `1678901234567890` |
| 3 | Processing Code | N6 | `100000` (GBP credit) |
| 4 | Amount Transaction | N12 | `000000050000` (£500.00) |
| 7 | Transmission Date & Time | N10 | `0518142530` |
| 11 | System Trace Audit Number | N6 | `123456` |
| 32 | Acquiring Institution ID | LLVAR N..11 | `SNDRUK22` |
| 37 | Retrieval Reference Number | AN12 | `REF123456789` |
| 41 | Terminal ID | ANS8 | `FPSGW01` |
| 42 | Card Acceptor ID | ANS15 | `UKFPSACCEPTOR` |
| 49 | Currency Code | N3 | `826` (GBP) |
| 100 | Receiving Institution ID | LLVAR N..11 | `BARCGB2L` |
| 102 | Account ID 1 (Sender) | LLVAR ANS..28 | `SNDRUK22ACCT` |
| 103 | Account ID 2 (Receiver) | LLVAR ANS..28 | `BARCGB2LACCT` |

### Bitmapa

Bit 1 (MSB pierwszego bajtu) = 1 oznacza obecność bitmapy sekundarnej (bity 65-128).
Sprawdzenie obecności DE: `1 << (64 - bit)` dla bitmapy primary, `1 << (128 - bit)` dla secondary.

### Kodowanie

RAMKA: 2 bajty big-endian długość payloadu (max 4096 bajtów).
MTI: 4 znaki ASCII.
Bitmapa primary: 8 bajtów (uint64 big-endian).
Bitmapa secondary: opcjonalne 8 bajtów.
DE: sekwencyjnie wg bitmapy.

### Response (0210)

DE39 (response code):
- `000` — Approved
- `051` — Insufficient funds
- `057` — Not permitted

Kwota DE4 jest konwertowana z pence na funty (`amount / 100.0`) przed rozliczeniem.

---

## Settlement DNS

FPS używa DNS — płatności przyjmowane przez cały dzień, rozliczane w cyklach:

```
Cykl 1:  08:00 - 10:00  →  rozliczenie 10:15
Cykl 2:  10:00 - 12:00  →  rozliczenie 12:15
Cykl 3:  12:00 - 14:00  →  rozliczenie 14:15
Cykl 4:  14:00 - 16:00  →  rozliczenie 16:15
Cykl 5:  16:00 - 18:00  →  rozliczenie 18:15
```

1. Obliczenie pozycji netto — dla każdego uczestnika: suma wychodzących minus przychodzących SIP
2. Raportowanie — każdy uczestnik otrzymuje swoją pozycję netto
3. Rozliczenie — uczestnicy z ujemną pozycją netto płacą; z dodatnią otrzymują środki

Harmonogram cykli konfigurowalny przez `config/sessions.json`.

---

## Gridlock resolution

Mechanizm identyczny jak w CHAPS: iteracja po QUEUED transakcjach, sprawdzenie dostępności płynności, rozliczenie kwalifikujących się.

Wywoływany:
- Automatycznie po zasileniu płynności
- Ręcznie przez `POST /v1/payments/fps/gridlock/resolve`

---

## Gniazdo TCP ISO 8583

FPS nasłuchuje na `:7421` (konfigurowalne przez `ISO8583_PORT`) dla połączeń TCP RAW.

Protokół ramki: 2 bajty big-endian długość, następnie payload ISO 8583 (max 4096 bajtów).
Gorutyna per połączenie. Handler TCP używa tych samych `Ledger.SettleSIP` i `Events.Publish`.

---

## Uczestnicy (seed)

| BIC | Nazwa | Saldo prefundowane |
|---|---|---|
| `BARCGB2L` | Barclays Bank | £500 000,00 |
| `HSBCGB44` | HSBC UK | £250 000,00 |
| `LLOYGB21` | Lloyds Bank | £375 000,00 |
| `SNDRUK22` | Alice Bank | £500 000,00 |
