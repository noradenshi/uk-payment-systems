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
		// Original Group Information is usually required before transaction details
		OrgnlGrpInfAndSts struct {
			OrgnlMsgId string `xml:"OrgnlMsgId"`
			OrgnlMsgNmId string `xml:"OrgnlMsgNmId"` // e.g., pacs.008.001.14
		} `xml:"OrgnlGrpInfAndSts"`
		TxInfAndSts struct {
			StsId           string `xml:"StsId"`
			OrgnlEndToEndId string `xml:"OrgnlEndToEndId"` // More common in .16 than OrgnlMsgId here
			TxSts           string `xml:"TxSts"`
			InstgAgt struct {
				FinInstnId struct {
					BICFI string `xml:"BICFI"`
				} `xml:"FinInstnId"`
			} `xml:"InstgAgt"`
			InstdAgt struct {
				FinInstnId struct {
					BICFI string `xml:"BICFI"`
				} `xml:"FinInstnId"`
			} `xml:"InstdAgt"`
		} `xml:"TxInfAndSts"`
	} `xml:"FIToFIPmtStsRpt"`
}

func NewPacs002(orgnlMsgId, e2eId, status, senderBic, receiverBic string) *Pacs002Message {
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
	
	m.PmtStsRpt.TxInfAndSts.InstgAgt.FinInstnId.BICFI = receiverBic 
	m.PmtStsRpt.TxInfAndSts.InstdAgt.FinInstnId.BICFI = senderBic
	
	return m
}
