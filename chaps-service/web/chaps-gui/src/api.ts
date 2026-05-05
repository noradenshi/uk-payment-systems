import type { DashboardData } from "./types";

const fallbackData: DashboardData = {
  metrics: [
    { label: "Rozliczone dziś", value: "28" },
    { label: "W kolejce", value: "4" },
    { label: "Odrzucone", value: "2" },
    { label: "Łączna wartość", value: "GBP 12.8M" },
  ],
  accounts: [
    { bic: "BARCGB2L", name: "Barclays Bank", balance: "GBP 1,000,000" },
    { bic: "HSBCGB44", name: "HSBC UK", balance: "GBP 500,000" },
    { bic: "LLOYGB21", name: "Lloyds Bank", balance: "GBP 750,000" },
    { bic: "SNDRUK22", name: "Bank Alice", balance: "GBP 1,000,000" }
  ],
  queue: [
    {
      msgId: "CHAPS-20260505-001",
      sender: "BARCGB2L",
      receiver: "HSBCGB44",
      amount: "GBP 250,000",
      status: "SETTLED",
      time: "09:08"
    },
    {
      msgId: "CHAPS-20260505-002",
      sender: "LLOYGB21",
      receiver: "BARCGB2L",
      amount: "GBP 1,200,000",
      status: "QUEUED",
      time: "09:34"
    },
    {
      msgId: "CHAPS-20260505-003",
      sender: "SNDRUK22",
      receiver: "HSBCGB44",
      amount: "GBP 85,000",
      status: "PENDING",
      time: "09:51"
    }
  ],
  services: [
    { label: "API CHAPS", state: "online", detail: "Nasłuch na :8080/pay" },
    { label: "Księga Postgres", state: "online", detail: "Konta i dziennik gotowe" },
    { label: "Kolejka rozliczeń", state: "degraded", detail: "GUI korzysta z danych demonstracyjnych" }
  ]
};

export async function getDashboardData(): Promise<DashboardData> {
  try {
    const response = await fetch("/pay", { method: "OPTIONS" });
    if (!response.ok && response.status !== 405) {
      return fallbackData;
    }

    return {
      ...fallbackData,
      services: fallbackData.services.map((service) =>
        service.label === "API CHAPS"
          ? { ...service, detail: "Dostępne z poziomu GUI", state: "online" }
          : service,
      ),
    };
  } catch {
    return fallbackData;
  }
}

export async function sendDemoPayment(xml: string): Promise<string> {
  try {
    const response = await fetch("/pay", {
      method: "POST",
      headers: {
        "Content-Type": "application/xml"
      },
      body: xml
    });

    if (!response.ok) {
      return `Brama zwróciła ${response.status}`;
    }

    const body = await response.text();
    const statusMatch = body.match(/<TxSts>(.*?)<\/TxSts>/);
    return statusMatch?.[1] ?? "Przetworzono";
  } catch {
    return "Tylko tryb demonstracyjny";
  }
}

export const samplePaymentXml = `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
  <FIToFICstmrCdtTrf>
    <GrpHdr>
      <MsgId>CHAPS-GUI-001</MsgId>
    </GrpHdr>
    <CdtTrfTxInf>
      <PmtId>
        <EndToEndId>CHAPS-E2E-001</EndToEndId>
        <TxId>CHAPS-TX-001</TxId>
      </PmtId>
      <IntrBkSttlmAmt Ccy="GBP">150000.00</IntrBkSttlmAmt>
    </CdtTrfTxInf>
  </FIToFICstmrCdtTrf>
</Document>`;
