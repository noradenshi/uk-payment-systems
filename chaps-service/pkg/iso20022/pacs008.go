package iso20022

import "encoding/xml"

type Pacs008Message struct {
	XMLName xml.Name `xml:"Document"`
	MsgId string `xml:"FIToFICstmrCdtTrf>GrpHdr>MsgId"`
	EndToEndId string `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>PmtId>EndToEndId"`

	Sender string `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>DbtrAgt>FinInstnId>BICFI"`
	DestBIC string `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>CdtrAgt>FinInstnId>BICFI"`
	Amount float64 `xml:"FIToFICstmrCdtTrf>CdtTrfTxInf>IntrBkSttlmAmt"`
}
