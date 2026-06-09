# UK Payment Systems (UKPS)

Wielousługowa symulacja brytyjskiej sieci płatności międzybankowych —
**CHAPS** (RTGS, wysokokwotowe), **FPS** (blisko-czasu-rzeczywistego,
niskokwotowe) i **BACS** (batchowe, 3-dniowe rozliczenie).

Każda usługa to niezależny binarny plik Go z własnym rejestrem PostgreSQL,
możliwy do wdrożenia razem przez Docker Compose. Nie ma współdzielonej bazy
danych ani bezpośredniej komunikacji międzyusługowej.

## Usługi

| Usługa | Schemat | Rozliczenie | Port aplikacji | Port DB | Nazwa DB |
| :--- | :--- | :--- | :--- | :--- | :--- |
| [CHAPS](./chaps-service/) | ISO 20022 | RTGS | `8080` | `5432` | `chaps_ledger` |
| [FPS](./fps-service/) | ISO 20022 + ISO 8583 | DNS | `8081` | `5433` | `fps_ledger` |
| [BACS](./bacs-service/) | Standard 18 | netting 3-dniowy | `8082` | `5434` | `bacs_ledger` |

## Szybki start

```bash
docker compose up -d
```

Powyższe polecenie uruchamia wszystkie trzy usługi i ich bazy danych. Pliki
README poszczególnych usług zawierają szczegółową dokumentację API, formaty
komunikatów i logikę rozliczeń.

## Architektura

```
bacs-service ─── Postgres 18 (bacs_ledger, port 5434)
fps-service  ─── Postgres 18 (fps_ledger, port 5433)
chaps-service ── Postgres 18 (chaps_ledger, port 5432)
```

Każda usługa jest całkowicie niezależna — osobny binarny plik, osobna baza
danych, osobny kontener. Nie ma współdzielonej infrastruktury. Patrz
`AGENTS.md` po konwencje implementacyjne.

## Integracje

- [KLIK → CHAPS](./docs/integrations/klik.md) — endpoint adaptera dla systemu KLIK (w języku angielskim)
- [KLIK → CHAPS (polski)](./docs/integrations/klik_pl.md) — polska wersja dokumentacji integracji KLIK
- [Przewodnik integracji bankowej](./docs/integrations/bank.md) — ogólny przewodnik dla banków (angielski)
- [Przewodnik integracji bankowej (polski)](./docs/integrations/bank_pl.md) — polska wersja przewodnika

## Więcej informacji

Szczegółowe konwencje kodowania, architektura i wskazówki dotyczące
rozszerzania systemu znajdują się w pliku `AGENTS.md`.
