package iso20022

import "encoding/xml"

type Pacs008Message struct {
	XMLName    xml.Name `xml:"Document"`
	MsgId      string   `xml:"FIToFICstmrCdtTrf>GrpHdr>MsgId"`
	EndToEndId string   `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>PmtId>EndToEndId"`

	Sender         string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>DbtrAgt>FinInstnId>BICFI"`
	SenderSortCode string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>DbtrAgt>FinInstnId>ClrSysMmbId>MmbId"`
	SenderIBAN     string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>DbtrAcct>Id>IBAN"`
	SenderAccount  string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>DbtrAcct>Id>Othr>Id"`
	DestBIC        string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>CdtrAgt>FinInstnId>BICFI"`
	DestSortCode   string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>CdtrAgt>FinInstnId>ClrSysMmbId>MmbId"`
	DestAccount    string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>CdtrAcct>Id>Othr>Id"`
	DestIBAN       string  `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>CdtrAcct>Id>IBAN"`
	Amount         float64 `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>IntrBkSttlmAmt"`
}

func (m *Pacs008Message) GetDebtorAccount() string {
	if m.SenderAccount != "" {
		return m.SenderAccount
	}
	return m.SenderIBAN
}

func (m *Pacs008Message) GetCreditorAccount() string {
	if m.DestAccount != "" {
		return m.DestAccount
	}
	return m.DestIBAN
}
