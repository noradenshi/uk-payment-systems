package iso20022

import (
	"encoding/xml"
	"time"
)

type Pacs002Message struct {
	XMLName xml.Name `xml:"Document"`
	Xmlns   string   `xml:"xmlns,attr"`
	PmtStsRpt struct {
		GrpHdr struct {
			MsgId   string    `xml:"MsgId"`
			CreDtTm time.Time `xml:"CreDtTm"`
		} `xml:"GrpHdr"`
		OrgnlGrpInfAndSts struct {
			OrgnlMsgId  string `xml:"OrgnlMsgId"`
			OrgnlMsgNmId string `xml:"OrgnlMsgNmId"`
		} `xml:"OrgnlGrpInfAndSts"`
		TxInfAndSts struct {
			StsId           string `xml:"StsId"`
			OrgnlEndToEndId string `xml:"OrgnlEndToEndId"`
			TxSts           string `xml:"TxSts"`
			StsRsnInf *StatusReasonInformation `xml:"StsRsnInf,omitempty"`
			InstgAgt FinancialAgent `xml:"InstgAgt"`
			InstdAgt FinancialAgent `xml:"InstdAgt"`
		} `xml:"TxInfAndSts"`
	} `xml:"FIToFIPmtStsRpt"`
}

type StatusReasonInformation struct {
	Rsn struct {
		Cd string `xml:"Cd"`
	} `xml:"Rsn"`
}

type FinancialAgent struct {
	FinInstnId struct {
		BICFI string `xml:"BICFI"`
	} `xml:"FinInstnId"`
}

func NewPacs002(orgnlMsgId, e2eId, status, senderBic, receiverBic string, reasonCode string) *Pacs002Message {
	m := &Pacs002Message{
		Xmlns: "urn:iso:std:iso:20022:tech:xsd:pacs.002.001.16",
	}

	now := time.Now().Truncate(time.Second)
	m.PmtStsRpt.GrpHdr.MsgId = "ACK-" + now.Format("20060102") + "-" + orgnlMsgId
	m.PmtStsRpt.GrpHdr.CreDtTm = now

	m.PmtStsRpt.OrgnlGrpInfAndSts.OrgnlMsgId = orgnlMsgId
	m.PmtStsRpt.OrgnlGrpInfAndSts.OrgnlMsgNmId = "pacs.008.001.14"

	m.PmtStsRpt.TxInfAndSts.StsId = "STAT-" + orgnlMsgId
	m.PmtStsRpt.TxInfAndSts.OrgnlEndToEndId = e2eId
	m.PmtStsRpt.TxInfAndSts.TxSts = status

	if reasonCode != "" {
		m.PmtStsRpt.TxInfAndSts.StsRsnInf = &StatusReasonInformation{}
		m.PmtStsRpt.TxInfAndSts.StsRsnInf.Rsn.Cd = reasonCode
	}

	m.PmtStsRpt.TxInfAndSts.InstgAgt.FinInstnId.BICFI = senderBic
	m.PmtStsRpt.TxInfAndSts.InstdAgt.FinInstnId.BICFI = receiverBic

	return m
}
