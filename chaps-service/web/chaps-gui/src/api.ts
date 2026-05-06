import type { DashboardData } from "./types";

const fallbackData: DashboardData = {
  metrics: [
    { label: "Rozliczone dzis", value: "0" },
    { label: "W kolejce", value: "0" },
    { label: "Odrzucone", value: "0" },
    { label: "Laczna wartosc", value: "GBP 0.00" },
  ],
  accounts: [],
  queue: [],
  services: [
    { label: "API CHAPS", state: "offline", detail: "Backend nie odpowiada" },
    { label: "Ksiega Postgres", state: "offline", detail: "Brak danych z bazy" },
    { label: "Kolejka rozliczen", state: "degraded", detail: "Czeka na polaczenie z backendem" },
  ],
};

export async function getDashboardData(): Promise<DashboardData> {
  try {
    const response = await fetch("/api/dashboard");
    if (!response.ok) {
      return fallbackData;
    }

    return response.json();
  } catch {
    return fallbackData;
  }
}

export async function sendDemoPayment(xml: string): Promise<string> {
  try {
    const response = await fetch("/pay", {
      method: "POST",
      headers: {
        "Content-Type": "application/xml",
      },
      body: xml,
    });

    const body = await response.text();
    if (!response.ok && response.status !== 202) {
      return body || `Brama zwrocila ${response.status}`;
    }

    const statusMatch = body.match(/<TxSts>(.*?)<\/TxSts>/);
    return statusMatch?.[1] ?? "Przetworzono";
  } catch {
    return "Backend nie odpowiada";
  }
}

export function createDemoPaymentXml(): string {
  const suffix = Date.now().toString();
  return samplePaymentXml
    .replace("CHAPS-GUI-001", `CHAPS-GUI-${suffix}`)
    .replace("CHAPS-E2E-001", `CHAPS-E2E-${suffix}`);
}

export const samplePaymentXml = `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
  <FIToFICstmrCdtTrf>
    <GrpHdr>
      <MsgId>CHAPS-GUI-001</MsgId>
      <CreDtTm>2026-05-06T09:00:00Z</CreDtTm>
      <NbOfTxs>1</NbOfTxs>
      <SttlmInf>
        <SttlmMtd>CLRG</SttlmMtd>
      </SttlmInf>
    </GrpHdr>
    <CdtTrfTxInf>
      <PmtId>
        <EndToEndId>CHAPS-E2E-001</EndToEndId>
      </PmtId>
      <IntrBkSttlmAmt Ccy="GBP">150000.00</IntrBkSttlmAmt>
      <ChrgBr>SLEV</ChrgBr>
      <Dbtr>
        <Nm>Alice Sender</Nm>
      </Dbtr>
      <DbtrAgt>
        <FinInstnId>
          <BICFI>SNDRUK22</BICFI>
        </FinInstnId>
      </DbtrAgt>
      <CdtrAgt>
        <FinInstnId>
          <BICFI>HSBCGB44</BICFI>
        </FinInstnId>
      </CdtrAgt>
      <Cdtr>
        <Nm>HSBC UK</Nm>
      </Cdtr>
    </CdtTrfTxInf>
  </FIToFICstmrCdtTrf>
</Document>`;
