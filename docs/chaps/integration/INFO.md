# CHAPS — Dokumentacja integracyjna

Dokument dla uczestników rozliczających się w systemie CHAPS — wysokowartościowych płatności RTGS w czasie rzeczywistym.

**Wersja:** 1.0
**Status:** Kompletny

---

## Spis treści

1. [Słownik domenowy](#słownik-domenowy)
2. [Obsługa błędów](#obsługa-błędów)
3. [API reference](#api-reference)
4. [Format ISO 20022](#format-iso-20022)
5. [Settlement RTGS](#settlement-rtgs)
6. [Gridlock resolution](#gridlock-resolution)
7. [Obsługa SSE](#obsługa-sse)

---

## Słownik domenowy

| Termin | Definicja |
|---|---|
| **CHAPS** | Clearing House Automated Payment System — brytyjski system RTGS dla płatności wysokowartościowych |
| **RTGS** | Real-Time Gross Settlement — każda płatność rozliczana indywidualnie i nieodwołalnie, bez nettingu |
| **Uczestnik bezpośredni** | Bank rozliczeniowy z kontem w Banku Anglii. W systemie występują wyłącznie uczestnicy bezpośredni |
| **BIC** | Bank Identifier Code — 8- lub 11-znakowy identyfikator banku (np. `BARCGB2L`) |
| **Sort Code** | 6-cyfrowy kod sortowania UK (format `XX-XX-XX` lub `XXXXXX`) |
| **pacs.008** | ISO 20022 komunikat płatniczy (FIToFICstmrCdtTrf) |
| **pacs.002** | ISO 20022 komunikat statusu (FIToFIPmtStsRpt) |
| **ACTC** | Accepted / Settled — płatność rozliczona |
| **PDNG** | Pending — płatność zakolejkowana z powodu braku płynności |
| **RJCT** | Rejected — płatność odrzucona |
| **QUEUED** | Stan transakcji czekającej na uwolnienie płynności |
| **Gridlock** | Zakleszczenie — cykliczna zależność, gdzie zakolejkowane płatności nie mogą się rozliczyć, ponieważ każdy nadawca czeka na środki od innego uczestnika |
| **Idempotencja** | Gwarancja że wielokrotne wysłanie tego samego `msg_id` nie spowoduje podwójnego rozliczenia |
| **BizMsg** | Własna koperta XML otaczająca `AppHdr` + `Document` |

---

## Obsługa błędów

Wszystkie błędy JSON mają format:

```json
{"error": "wiadomość błędu"}
```

| Kod HTTP | Kategoria | Kiedy występuje |
|---|---|---|
| 200 | Sukces | Płatność przyjęta i rozliczona (ACTC) |
| 202 | Zaakceptowano | Płatność zakolejkowana (PDNG) lub odrzucona (RJCT) |
| 400 | Błąd walidacji | Brakujące pole, nieprawidłowy BIC, kwota ≤ 0 |
| 404 | Nie znaleziono | Transakcja o podanym ID nie istnieje |
| 409 | Konflikt | Próba anulowania już rozliczonej płatności |
| 415 | Nieobsługiwany typ | Należy użyć `application/json` lub `application/xml` |
| 500 | Błąd wewnętrzny | Nieoczekiwany błąd serwera |
| 503 | Niedostępny | Poza godzinami operacyjnymi lub system w konserwacji |

---

## API reference

**Bazowy URL:** `http://localhost:8080/v1`

### Płatności

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/payments/chaps` | Zainicjuj płatność CHAPS. Akceptuje ISO 20022 XML lub JSON |
| **POST** | `/payments/chaps/validate` | Walidacja sucha: BIC, status uczestnika, płynność |
| **GET** | `/payments/chaps/{id}` | Status płatności, szczegóły ISO 20022, audyt |
| **GET** | `/payments/chaps` | Lista/filtrowanie płatności po statusie i limicie |
| **POST** | `/payments/chaps/{id}/authorize` | Autoryzacja 2FA dla płatności wysokowartościowych (stub) |
| **DELETE** | `/payments/chaps/{id}` | Anulowanie płatności — tylko gdy status `PENDING` |
| **POST** | `/payments/chaps/{id}/amend` | Zmiana szczegółów płatności oczekującej |
| **POST** | `/payments/chaps/gridlock/resolve` | Ręczne wywołanie rozwiązywania gridlocka |
| **GET** | `/payments/chaps/limits` | Limity rozliczeniowe i pozostała płynność dzienna |

### Uczestnicy

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/participants` | Lista uczestników |
| **POST** | `/participants/register` | Rejestracja nowego uczestnika |
| **PATCH** | `/participants/{bic}/status` | Zmiana statusu (ACTIVE/SUSPENDED/DISABLED) |
| **POST** | `/participants/{bic}/block` | Blokada awaryjna (kill-switch) |
| **GET** | `/participants/{bic}/block` | Szczegóły blokady |
| **DELETE** | `/participants/{bic}/block` | Odblokowanie |
| **GET** | `/participants/{bic}/positions` | Pozycja w czasie rzeczywistym |

### Płynność

| Metoda | Endpoint | Opis |
|---|---|---|
| **POST** | `/liquidity/top-up` | Symulacja zasilenia z banku centralnego |

### System

| Metoda | Endpoint | Opis |
|---|---|---|
| **GET** | `/system/schedule` | Godziny operacyjne i cut-off |
| **GET** | `/payments/chaps/incoming/{bic}` | SSE — strumień zdarzeń w czasie rzeczywistym |

---

## Format ISO 20022

### Koperta BizMsg

Płatności przychodzą jako XML z kopertą `BizMsg` zawierającą `AppHdr` i `Document`:

```xml
<BizMsg>
  <AppHdr xmlns="urn:iso:std:iso:20022:tech:xsd:head.001.001.02">
    <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
    <To><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></To>
    <BizMsgIdr>BAH-REF-001</BizMsgIdr>
    <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
    <CreDt>2026-06-03T14:30:00Z</CreDt>
  </AppHdr>
  <Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
    <FIToFICstmrCdtTrf>
      <GrpHdr>
        <MsgId>CHAPS-20260603-001</MsgId>
      </GrpHdr>
      <CdtTrfTxInf>
        <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
        <IntrBkSttlmAmt Ccy="GBP">15000000.00</IntrBkSttlmAmt>
        <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
        <CdtrAgt><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></CdtrAgt>
      </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
  </Document>
</BizMsg>
```

### Walidacja XSD

Przed parsowaniem koperta przechodzi walidację XSD przez `chaps_wrapper.xsd` (libxml2):

1. Walidacja koperty `BizMsg` względem `chaps_wrapper.xsd`
2. Ekstrakcja `MsgDefIdr` przez XPath → routing do właściwego schematu
3. Ekstrakcja `<Document>` przez XPath
4. `xml.Unmarshal` do struktury `Pacs008Message`
5. Walidacja biznesowa (BIC, kwota, długość MsgId)

### Odpowiedź (pacs.002)

```xml
<BizMsg>
  <AppHdr>
    <Fr><FIId><FinInstnId><BICFI>BARCGB2L</BICFI></FinInstnId></FIId></Fr>
    <To><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></To>
    <BizMsgIdr>BAH-ACK-...</BizMsgIdr>
    <MsgDefIdr>pacs.002.001.16</MsgDefIdr>
    <CreDt>2026-06-03T14:30:01Z</CreDt>
  </AppHdr>
  <Document>
    <FIToFIPmtStsRpt>
      <TxInfAndSts>
        <TxSts>ACTC</TxSts>
      </TxInfAndSts>
    </FIToFIPmtStsRpt>
  </Document>
</BizMsg>
```

Nagłówek odpowiedzi zawiera `X-Transaction-Status` dla szybkiej inspekcji.

---

## Settlement RTGS

### Przebieg rozliczenia

```
1. INSERT transakcji (PENDING) z ON CONFLICT (msg_id) — brama idempotencji
   → Jeśli już SETTLED: zwróć zapisany ACTC

2. Blokada wiersza statusu nadawcy (FOR UPDATE)
   → Odrzuć jeśli nie ACTIVE lub is_closed

3. Blokada wiersza płynności nadawcy (FOR UPDATE)
   → Jeśli niewystarczające saldo: ustaw QUEUED, zwróć PDNG

4. Wykonanie gross settlement:
   → Debet nadawcy: balance -= amount
   → Kredyt odbiorcy: balance += amount

5. Rejestracja wpisów w dzienniku (2 wiersze):
   → Wpis nadawcy: kwota ujemna (debet)
   → Wpis odbiorcy: kwota dodatnia (kredyt)
   → pg_notify('liquidity_event', bic_odbiorcy) na triggerze INSERT

6. Finalizacja: status = SETTLED
```

### Idempotencja

```sql
INSERT INTO transactions (msg_id, sender_bic, receiver_bic, amount, status)
VALUES ($1, $2, $3, $4, 'PENDING')
ON CONFLICT (msg_id) DO UPDATE SET msg_id = EXCLUDED.msg_id
RETURNING id, status
```

Jeśli `status` = `SETTLED`, transakcja jest pomijana i zwracany jest zapisany ACTC.

### Limity

| Limit | Wartość |
|---|---|
| Waluta | GBP |
| Pojedyncza płatność | £20 000 000 |
| Limit dzienny uczestnika | £100 000 000 |

### Godziny operacyjne

| Okno | Czas (London) |
|---|---|
| Otwarcie systemu | 06:00 |
| Cut-off kliencki | 17:40 |
| Cut-off międzybankowy | 18:00 |

Poza godzinami operacyjnymi endpoint `POST /payments/chaps` zwraca HTTP 503.

---

## Gridlock resolution

Gridlock powstaje gdy wiele płatności QUEUED czeka na wzajemne uwolnienie płynności.

Algorytm rozwiązywania (`ResolveGridlock`):

1. Pobranie wszystkich transakcji QUEUED w kolejności utworzenia
2. Dla każdej: sprawdzenie czy saldo nadawcy (z uwzględnieniem overdraftu) pokrywa kwotę
3. Jeśli tak — rozliczenie transakcji (debet/kredyt + wpisy dziennika)
4. Powtarzanie aż do braku dalszego postępu

Uruchamiane:
- **Automatycznie** po każdym zasileniu płynności (`POST /liquidity/top-up`)
- **Ręcznie** przez `POST /v1/payments/chaps/gridlock/resolve`

---

## Obsługa SSE

Endpoint `GET /v1/payments/chaps/incoming/{bic}` zwraca strumień Server-Sent Events.

Zdarzenia publikowane przy każdej płatności ACTC:

```
data: {"type":"payment.received","data":{"msg_id":"...","sender":"SNDRUK22","receiver":"BARCGB2L","amount":15000000,"status":"SETTLED","scheme":"CHAPS"}}
```

- Wykorzystuje in-memory `EventBus` (mapa BIC → kanały)
- Buforowane kanały (100 zdarzeń), nadmiar odrzucany
- Rozłączenie klienta wykrywane przez `r.Context().Done()`

---

## Uczestnicy (seed)

| BIC | Nazwa | Saldo początkowe |
|---|---|---|
| `BARCGB2L` | Barclays Bank | £1 000 000,00 |
| `HSBCGB44` | HSBC UK | £500 000,00 |
| `LLOYGB21` | Lloyds Bank | £750 000,00 |
| `SNDRUK22` | Alice Bank | £1 000 000,00 |
