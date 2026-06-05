# UK Payment Systems (UKPS)

Wielousługowa symulacja brytyjskiej sieci płatności międzybankowych — **CHAPS** (RTGS, wysokokwotowe), **FPS** (blisko-czasu-rzeczywistego, niskokwotowe) i **BACS** (batchowe, 3-dniowe rozliczenie) — oraz panel nadzorczy **Banku Centralnego**.

Każda usługa to niezależny binarny plik Go z własnym rejestrem PostgreSQL, możliwy do wdrożenia razem przez Docker Compose.

## Usługi

| Usługa | Schemat | Rozliczenie | Port aplikacji | Port DB | Nazwa DB |
| :--- | :--- | :--- | :--- | :--- | :--- |
| [CHAPS](./chaps-service/) | ISO 20022 | RTGS | `8420` | `5420` | `chaps_ledger` |
| [FPS](./fps-service/) | ISO 20022 + ISO 8583 | DNS | `8421` | `5421` | `fps_ledger` |
| [BACS](./bacs-service/) | Standard 18 | netting 3-dniowy | `8422` | `5422` | `bacs_ledger` |

## Szybki start

```bash
docker compose up -d
```

Powyższe polecenie uruchamia wszystkie cztery usługi i ich bazy danych. Pliki README poszczególnych usług zawierają szczegółową dokumentację API, formaty komunikatów i logikę rozliczeń.

## Architektura

```
bacs-service ─── Postgres 18 (bacs_ledger)
fps-service  ─── Postgres 18 (fps_ledger)
chaps-service ── Postgres 18 (chaps_ledger)
```

Każda usługa jest całkowicie niezależna — osobny binarny plik, osobna baza danych, osobny kontener. Koncepcyjnie współdzielą wzorzec wspólnego rejestru uczestników, ale nie współdzielą infrastruktury. Patrz `AGENTS.md` po konwencje implementacyjne.

## Integracje

- [KLIK → CHAPS](./docs/integrations/klik.md) — endpoint adaptera dla systemu KLIK
