# Przewodnik integracji bankowej — UK Payment Systems (UKPS)

## Przegląd

UKPS to symulator wieloschematowego systemu płatności międzybankowych. Trzy
niezależne usługi działają równolegle, każda modelująca rzeczywisty brytyjski
system płatniczy:

| Usługa | Schemat | Rozliczenie | Wartość docelowa | Port |
| :--- | :--- | :--- | :--- | :--- |
| CHAPS | Real-Time Gross Settlement (RTGS) | Natychmiastowe, nieodwołalne | Wysokokwotowe (£1M+) | 8080 |
| FPS | Faster Payments | Blisko-czasu-rzeczywistego (SIP) + DNS | Niskokwotowe (<£250K) | 8081 |
| BACS | BACS | Batchowe, 3-dniowy cykl | Niskokwotowe, duży wolumen | 8082 |

Każda usługa uruchamia własną instancję PostgreSQL z niezależnym rejestrem
uczestników. Nie ma współdzielonej bazy danych ani bezpośredniej komunikacji
międzyusługowej.

---

## Uczestniczące Banki (seed)

Cztery banki są zasilane we wszystkich trzech usługach. Każda usługa przechowuje
własną kopię z saldami i atrybutami specyficznymi dla schematu.

### Zarejestrowani Uczestnicy

| Bank | BIC | Sort Code |
| :--- | :--- | :--- |
| Barclays Bank | `BARCGB2L` | `20-00-00` |
| HSBC UK | `HSBCGB44` | `40-00-00` |
| Lloyds Bank | `LLOYGB21` | `30-00-00` |
| Alice Bank | `SNDRUK22` | `60-00-00` |

### Salda według Schematu

| Bank | CHAPS | FPS | BACS |
| :--- | :--- | :--- | :--- |
| Barclays | £1 000 000 | £500 000 | £1 000 000 |
| HSBC | £500 000 | £300 000 | £800 000 |
| Lloyds | £750 000 | £400 000 | £750 000 |
| Alice Bank | £1 000 000 | £500 000 | £500 000 |

### Uwierzytelnianie

Wszystkie endpointy API (z wyjątkiem `GET /v1/healthz`) wymagają klucza API
przesłanego w nagłówku `Authorization`:

```
Authorization: Bearer <api_key>
```

Klucz API identyfikuje bank wywołujący (BIC). W ścieżkach nie ma parametru
`{bic}` — BIC jest wyprowadzany z klucza API po stronie serwera.

Klucze API są generowane automatycznie podczas rejestracji. 4 banki seed
mają predefiniowane klucze (patrz Klucze API Seed poniżej).

### Rejestracja Nowego Banku

```
POST /v1/participants/register
Content-Type: application/json

{
  "bic": "ABCDGB2L",
  "name": "Nowy Bank",
  "sort_code": "12-34-56",
  "balance": 500000.00
}
```

Odpowiedź:

```json
{
  "bic": "ABCDGB2L",
  "api_key": "ukps_3a1f2b8c4d7e9f0a1b2c3d4e5f6a7b8c",
  "status": "ACTIVE"
}
```

Klucz `api_key` jest zwracany **tylko raz** podczas rejestracji. Przechowuj
go bezpiecznie.

### Klucze API Seed

4 banki seed mają następujące predefiniowane klucze API:

| Bank | BIC | Klucz API |
| :--- | :--- | :--- |
| Barclays | `BARCGB2L` | `ukps_chaps_key_barclays` / `ukps_fps_key_barclays` / `ukps_bacs_key_barclays` |
| HSBC | `HSBCGB44` | `ukps_chaps_key_hsbc` / `ukps_fps_key_hsbc` / `ukps_bacs_key_hsbc` |
| Lloyds | `LLOYGB21` | `ukps_chaps_key_lloyds` / `ukps_fps_key_lloyds` / `ukps_bacs_key_lloyds` |
| Alice Bank | `SNDRUK22` | `ukps_chaps_key_alice` / `ukps_fps_key_alice` / `ukps_bacs_key_alice` |

Każda usługa ma własny zestaw kluczy API. Użyj klucza specyficznego dla
schematu przy wywoływaniu danej usługi (np. `ukps_chaps_key_barclays` dla
CHAPS na porcie 8080).

`sort_code` jest wymagany we wszystkich trzech usługach. Dodatkowe pola
specyficzne dla schematu:

| Usługa | Dodatkowe pola |
|---|---|---|
| CHAPS | `sort_code` |
| FPS | `sort_code`, `participant_type` (`DIRECT`/`INDIRECT`), `sponsor_bic` |
| BACS | `sort_code`, `su_code`, `is_service_user`, `is_destination_user` |

**Sort code jest wymagany** przy rejestracji uczestnika we wszystkich trzech
usługach. Te różnice odzwierciedlają rzeczywiste koncepcje domenowe brytyjskich
systemów płatniczych:
- **Sort code** — uniwersalny identyfikator routingu w UK (używany przez
  wszystkie trzy schematy)
- **Participant type + sponsor BIC** — FPS posiada model sponsoringu
  (uczestnicy pośredni routing przez bezpośrednich)
- **Flagi SU/DSU** — BACS rozróżnia Service Users (inicjatorów) od
  Destination Service Users (odbiorców)

---

## Formaty Komunikatów

Każdy schemat obsługuje standardowy format międzybankowy. JSON jest dostępny
jako alternatywa dla wszystkich schematów — przydatny do testowania i narzędzi
wewnętrznych, ale niezalecany dla produkcyjnego ruchu międzybankowego.

| Schemat | Format Standardowy | Alternatywa JSON |
| :--- | :--- | :--- |
| CHAPS | ISO 20022 XML (pacs.008) | ✅ — składanie płatności + zarządzanie |
| FPS | ISO 20022 XML (pacs.008) **lub** ISO 8583 binarny | ✅ — składanie płatności + zarządzanie |
| BACS | Standard 18 (stała szerokość ASCII) | ✅ — tylko zarządzanie (cykle, mandaty, raporty, uczestnicy). Przesyłanie plików wymaga Standard 18. |

### ISO 20022 XML (CHAPS i FPS)

Płatności przychodzą jako koperta `BizMsg` zawierająca `AppHdr` + `Document`.

```xml
<BizMsg>
  <AppHdr>
    <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
    <To><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></To>
    <BizMsgIdr>BAH-REF-001</BizMsgIdr>
    <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
    <CreDt>2026-06-09T10:00:00Z</CreDt>
  </AppHdr>
  <Document>
    <FIToFICstmrCdtTrf>
      <GrpHdr>
        <MsgId>PAY-REF-001</MsgId>
      </GrpHdr>
      <CdtTrfTxInf>
        <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
        <IntrBkSttlmAmt Ccy="GBP">5000.00</IntrBkSttlmAmt>
        <DbtrAgt>
          <FinInstnId>
            <BICFI>SNDRUK22</BICFI>
            <ClrSysMmbId><MmbId>60-00-00</MmbId></ClrSysMmbId>
          </FinInstnId>
        </DbtrAgt>
        <CdtrAgt>
          <FinInstnId>
            <BICFI>BARCGB2L</BICFI>
            <ClrSysMmbId><MmbId>20-00-00</MmbId></ClrSysMmbId>
          </FinInstnId>
        </CdtrAgt>
      </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
  </Document>
</BizMsg>
```

Zarówno CHAPS, jak i FPS walidują przychodzący XML względem schematów XSD
przez libxml2 (wymaga CGO). CHAPS używa `chaps_wrapper.xsd` do walidacji
koperty; FPS waliduje poszczególne schematy (pacs.008, head.001 itd.).

Wszystkie trzy schematy używają brytyjskich sort code, które są **wymagane**
w ISO 20022 XML (wewnątrz `FinInstnId` > `ClrSysMmbId` > `MmbId`). Brak
sort code skutkuje odrzuceniem `RJCT`/`SORT-CODE-MISSING`.

Odpowiedź to `pacs.002.001.16` ze statusem: `ACTC` (rozliczone), `PDNG`
(zakolejkowane) lub `RJCT` (odrzucone).

### ISO 8583 Binarny (Tylko FPS)

Komunikaty ISO 8583 używają 2-bajtowego prefiksu długości (big-endian)
na porcie TCP `7421`. Obsługiwane MTI: 0200 (zapytanie) / 0210 (odpowiedź).

Kluczowe elementy danych:

| DE | Nazwa | Format | Przykład |
| :--- | :--- | :--- | :--- |
| 4 | Kwota | N12 (pensy) | `000000500000` (£5000.00) |
| 11 | Numer referencyjny | N6 | `123456` |
| 32 | BIC nabywcy | LLVAR N..11 | `SNDRUK22` |
| 100 | BIC odbiorcy | LLVAR N..11 | `BARCGB2L` |

Kody odpowiedzi DE39: `000` (zatwierdzone), `051` (brak środków), `057`
(niedozwolone).

### Standard 18 (Tylko BACS)

Stała szerokość ASCII, 80 znaków na rekord, zakończone znakiem nowej linii.
Typy rekordów: 1 (nagłówek wolumenu), 3 (polecenie zapłaty), 4 (uznanie),
5 (przyczepa), 9 (przyczepa użytkownika), A (mandat AUDDIS).

Kwoty pieniężne są w pensach (liczby całkowite). Parser dzieli przez 100.0
aby uzyskać wartości w GBP. Sort code występują w Record 3 (`DestSortCode`
na pozycjach 8–16, `OriginatorSortAcc` na 37–51) i Record 4 (`DestSortCode`
na 8–16).

### JSON (Wszystkie Schematy — Format Alternatywny)

Płatności JSON używają tych samych pól dla CHAPS i FPS. Sort code
(`sender_sort_code`, `receiver_sort_code`) są **wymagane** — użyj ISO 8583
dla wiadomości bez sort code.

```json
POST /v1/payments/chaps
Content-Type: application/json
Authorization: Bearer ukps_chaps_key_alice

{
  "receiver_bic": "BARCGB2L",
  "amount": 5000.00,
  "msg_id": "PAY-REF-001",
  "receiver_sort_code": "20-00-00"
}
```

BIC nadawcy i sort code nadawcy są wyprowadzane z klucza API —
**nie** są dołączane w ciele JSON.

Endpoint JSON FPS akceptuje identyczną strukturę. BACS **nie** akceptuje
JSON do przesyłania plików (użyj Standard 18), ale używa JSON do wszystkich
endpointów zarządzania: cykle, mandaty, zwroty, raporty, uczestnicy i limity.

---

## API — Referencja

### Wspólne Endpointy (Wszystkie Schematy)

Te endpointy działają identycznie we wszystkich trzech usługach:

| Metoda | Ścieżka | Opis |
| :--- | :--- | :--- |
| `GET` | `/v1/healthz` | Health check (bez uwierzytelniania) |
| `GET` | `/v1/participants` | Lista wszystkich zarejestrowanych uczestników |
| `POST` | `/v1/participants/register` | Rejestracja nowego banku (zwraca `api_key`) |
| `PATCH` | `/v1/participants/status` | Aktualizacja własnego statusu (ACTIVE/SUSPENDED/DISABLED) |
| `POST` | `/v1/participants/block` | Blokada awaryjna (własna) |
| `GET` | `/v1/participants/block` | Szczegóły własnej blokady |
| `DELETE` | `/v1/participants/block` | Odblokowanie (własne) |
| `GET` | `/v1/participants/positions` | Własna pozycja w czasie rzeczywistym |
| `POST` | `/v1/liquidity/top-up` | Zastrzyk płynności z banku centralnego |
| `GET` | `/v1/system/schedule` | Godziny pracy / harmonogram cykli |

> Wszystkie endpointy z wyjątkiem `GET /v1/healthz` i `POST /v1/participants/register` wymagają `Authorization: Bearer <api_key>`.

Odpowiedzi błędów: `{"error": "message"}`. Błędy walidacji XSD dotyczą
CHAPS i FPS (CGO + libxml2).

### CHAPS — Endpointy Płatności

| Metoda | Ścieżka | Opis |
| :--- | :--- | :--- |
| `POST` | `/v1/payments/chaps` | Złóż płatność (XML lub JSON) |
| `POST` | `/v1/payments/chaps/validate` | Walidacja testowa |
| `GET` | `/v1/payments/chaps` | Lista własnych płatności |
| `GET` | `/v1/payments/chaps/{id}` | Status płatności |
| `POST` | `/v1/payments/chaps/{id}/authorize` | Zatwierdzenie 2FA (stub) |
| `GET` | `/v1/payments/chaps/incoming` | SSE strumień czasu rzeczywistego (BIC z auth) |

### FPS — Endpointy Płatności

| Metoda | Ścieżka | Opis |
| :--- | :--- | :--- |
| `POST` | `/v1/payments/fps` | Złóż SIP (XML, JSON lub ISO 8583 przez HTTP) |
| `POST` | `/v1/payments/fps/iso8583` | ISO 8583 binarny przez HTTP |
| `POST` | `/v1/payments/fps/forward-dated` | Zaplanuj przyszłą płatność |
| `POST` | `/v1/payments/fps/standing-orders` | Utwórz polecenie zapłaty stałej |
| `POST` | `/v1/payments/fps/bulk` | Prześlij plik płatności zbiorczych |
| `GET` | `/v1/payments/fps/incoming` | SSE strumień czasu rzeczywistego (BIC z auth) |
| `GET` | `/v1/settlement/dns/cycle` | Aktualny cykl DNS |
| `POST` | `/v1/settlement/dns/close` | Wymuś rozliczenie DNS |

### BACS — Endpointy Płatności

| Metoda | Ścieżka | Opis |
| :--- | :--- | :--- |
| `POST` | `/v1/payments/bacs/submit` | Prześlij plik Standard 18 |
| `GET` | `/v1/payments/bacs/cycle/current` | Informacje o bieżącym cyklu |
| `POST` | `/v1/payments/bacs/cycle/close` | Zamknij dzień wejściowy |
| `POST` | `/v1/payments/bacs/cycle/settle` | Rozlicz cykl |
| `POST` | `/v1/payments/bacs/mandates` | Utwórz mandat AUDDIS |
| `GET` | `/v1/payments/bacs/reports/{cycle-date}` | Raporty rozliczeniowe |
| `GET` | `/v1/payments/bacs/incoming` | SSE strumień czasu rzeczywistego (BIC z auth) |

---

## Zdarzenia Czasu Rzeczywistego (SSE)

Każda usługa udostępnia endpoint server-sent events do powiadomień w czasie
rzeczywistym.

| Usługa | Endpoint | Typ Zdarzenia | Kiedy |
| :--- | :--- | :--- | :--- |
| CHAPS | `GET /v1/payments/chaps/incoming` | `payment.received` | Przy każdym rozliczeniu ACTC |
| FPS | `GET /v1/payments/fps/incoming` | `payment.received` | Przy każdym rozliczeniu ACTC |
| BACS | `GET /v1/payments/bacs/incoming` | `cycle.settled` | Przy rozliczeniu cyklu |

Uwierzytelnianie jest wymagane. BIC jest wyprowadzany z nagłówka
`Authorization`, więc klienci otrzymują tylko zdarzenia adresowane do
ich własnego BIC.

Zdarzenia są w pamięci (bez trwałości). Klienci, którzy połączą się po
opublikowaniu zdarzenia, nie otrzymają go. Standardowy format SSE:

```
data: {"type":"payment.received","data":{"msg_id":"...","amount":5000.00,...}}\n\n
```

---

## Godziny Pracy

Każdy schemat egzekwuje godziny pracy na poziomie handlera:

| Schemat | Okno | Zachowanie poza oknem |
| :--- | :--- | :--- |
| CHAPS | 06:00–18:00 (cut-off międzybankowy) | HTTP 503 |
| FPS | Konfigurowalny czas otwarcia/zamknięcia | HTTP 503 lub ISO 8583 DE39=91 |
| BACS | Cut-off wejściowy (domyślnie 22:30) | HTTP 503 przy przesyłaniu pliku |

BACS działa w 3-dniowym cyklu niezależnie od godzin pracy (przesyłanie
plików jest bramkowane tylko przez `input_cutoff`).

W trybie demo, czasy trwania cykli i interwały tick są kompresowane zgodnie
z `demo_session_minutes` w `config/sessions.json`.

---

## Obsługa Błędów

### Kody Statusu HTTP

| Kod | Znaczenie |
| :--- | :--- |
| 200 | Sukces (rozliczone) |
| 201 | Utworzone (submisja zaakceptowana) |
| 202 | Zaakceptowane (zakolejkowane lub oczekujące) |
| 400 | Złe żądanie (nieprawidłowy format, brakujące pola) |
| 404 | Nie znaleziono (nieznany BIC, transakcja, cykl) |
| 409 | Konflikt (duplikat msg_id z innymi danymi) |
| 500 | Błąd wewnętrzny |
| 503 | Usługa niedostępna (poza godzinami pracy) |

### Kody Przyczyn

| Kontekst | Kod | Znaczenie |
| :--- | :--- | :--- |
| ISO 20022 | `ACTC` | Zaakceptowane / rozliczone |
| ISO 20022 | `PDNG` z `INSU` | Zakolejkowane — brak płynności |
| ISO 20022 | `RJCT` z `AC01` | Nieznane konto / BIC |
| ISO 20022 | `RJCT` z `AC04` | Konto zamknięte lub zablokowane |
| ISO 20022 | `RJCT` z `XMLI` | Nieprawidłowy schemat XSD |
| ISO 20022 | `RJCT` z `SORT-CODE-MISSING` | Brak sort code w XML lub JSON |
| ISO 8583 | DE39=`000` | Zatwierdzone |
| ISO 8583 | DE39=`051` | Brak środków |
| ISO 8583 | DE39=`057` | Niedozwolone |

---

## Przewodnik Połączenia

### Docker Compose

Każda usługa ma produkcyjny `compose.yml` i deweloperski `compose-dev.yml`.
Główny `compose.yml` w katalogu głównym orkiestruje wszystkie trzy usługi
razem.

```yaml
services:
  chaps-app:
    build: ./chaps-service
    ports: ["8080:8080"]
    environment:
      DATABASE_URL: postgres://chaps_admin:password123@chaps-db:5432/chaps_ledger
    depends_on: [chaps-db]

  chaps-db:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: chaps_ledger
      POSTGRES_USER: chaps_admin
      POSTGRES_PASSWORD: password123
```

### Sieć

Usługi są izolowane. Aby podłączyć zewnętrzny system bankowy do konkretnego
schematu, umieść kontener w tej samej sieci Docker co docelowa usługa.

### Zmienne Środowiskowe

| Zmienna | Usługi | Domyślnie |
| :--- | :--- | :--- |
| `DATABASE_URL` | Wszystkie | Ciąg połączenia Postgres specyficzny dla schematu |
| `ISO8583_PORT` | FPS | `:7421` |
| `PORT` | Wszystkie | `8080`/`8081`/`8082` |

### Konfiguracja

Harmonogramy rozliczeń, godziny pracy, interwały tick i tryb demo są
kontrolowane przez `config/sessions.json` w każdej usłudze.

---

## Przykładowe Przepływy

### CHAPS — Płatność wysokokwotowa RTGS (ISO 20022 XML)

```bash
curl -X POST http://localhost:8080/v1/payments/chaps \
  -H "Content-Type: application/xml" \
  -H "Authorization: Bearer ukps_chaps_key_alice" \
  -d '<BizMsg>
    <AppHdr>
      <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
      <To><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></To>
      <BizMsgIdr>BAH-001</BizMsgIdr>
      <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
      <CreDt>2026-06-09T10:00:00Z</CreDt>
    </AppHdr>
    <Document>
      <FIToFICstmrCdtTrf>
        <GrpHdr><MsgId>CHAPS-001</MsgId></GrpHdr>
        <CdtTrfTxInf>
          <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
          <IntrBkSttlmAmt Ccy="GBP">15000000.00</IntrBkSttlmAmt>
          <DbtrAgt>
            <FinInstnId>
              <BICFI>SNDRUK22</BICFI>
              <ClrSysMmbId><MmbId>60-00-00</MmbId></ClrSysMmbId>
            </FinInstnId>
          </DbtrAgt>
          <CdtrAgt>
            <FinInstnId>
              <BICFI>BARCGB2L</BICFI>
              <ClrSysMmbId><MmbId>20-00-00</MmbId></ClrSysMmbId>
            </FinInstnId>
          </CdtrAgt>
        </CdtTrfTxInf>
      </FIToFICstmrCdtTrf>
    </Document>
  </BizMsg>'
```

### FPS — SIP przez JSON

```bash
curl -X POST http://localhost:8081/v1/payments/fps \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ukps_fps_key_alice" \
  -d '{"receiver_bic":"BARCGB2L","amount":250.00,"msg_id":"FPS-001","receiver_sort_code":"20-00-00"}'
```

### FPS — SIP przez gniazdo TCP ISO 8583

```python
import socket, struct

def build_0200():
    buf = b'0200'
    prim = (1 << (64-4)) | (1 << (64-11)) | (1 << (64-32)) | (1 << 63)
    sec  = 1 << (128-100)
    buf += struct.pack('>Q', prim) + struct.pack('>Q', sec)
    buf += b'000000002500'         # £25.00 (2500 pensów)
    buf += b'123456'               # trace
    buf += struct.pack('B', 8) + b'SNDRUK22'
    buf += struct.pack('B', 8) + b'BARCGB2L'
    return buf

s = socket.socket()
s.connect(('localhost', 7421))
msg = build_0200()
s.send(struct.pack('>H', len(msg)) + msg)
resp_len = struct.unpack('>H', s.recv(2))[0]
resp = s.recv(resp_len)
print('0210:', resp.hex())
```

### BACS — Przesłanie pliku Standard 18

```bash
curl -X POST http://localhost:8082/v1/payments/bacs/submit \
  -H "Content-Type: text/plain" \
  -H "Authorization: Bearer ukps_bacs_key_barclays" \
  -d '1{SUCODE01  }0000001BARCLAYS TEST        30000100000123456'
```

### BACS — Cykl życia rozliczenia

```
Dzień 1: POST /v1/payments/bacs/submit          → plik zaakceptowany
         POST /v1/payments/bacs/cycle/close     → OPEN → PROCESSING
Dzień 2: (automatyczne lub ręczne przejście)     → PROCESSING → AWAITING_SETTLEMENT
Dzień 3: POST /v1/payments/bacs/cycle/settle     → AWAITING_SETTLEMENT → SETTLED
         GET  /v1/payments/bacs/reports/{date}   → raport rozliczeniowy
```
