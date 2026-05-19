package iso20022

import (
	"encoding/xml"
	"time"
)

type BusinessMessage struct {
	XMLName  xml.Name `xml:"BizMsg"`
	AppHdr   AppHdr   `xml:"AppHdr"`
	Document any      `xml:"Document"`
}

type AppHdr struct {
	Xmlns     string    `xml:"xmlns,attr"`
	Fr        Party     `xml:"Fr"`
	To        Party     `xml:"To"`
	BizMsgIdr string    `xml:"BizMsgIdr"`
	MsgDefIdr string    `xml:"MsgDefIdr"`
	CreDt     time.Time `xml:"CreDt"`
	Sgntr     *Signature `xml:"Sgntr,omitempty"`
}

type Party struct {
	FIId struct {
		FinInstnId struct {
			BICFI string `xml:"BICFI"`
		} `xml:"FinInstnId"`
	} `xml:"FIId"`
}

type Signature struct {
	Any string `xml:",innerxml"`
}

func NewBAH(from, to, msgId, msgType string) AppHdr {
	return AppHdr{
		Xmlns:     "urn:iso:std:iso:20022:tech:xsd:head.001.001.04",
		BizMsgIdr: "BAH-" + msgId,
		MsgDefIdr: msgType,
		CreDt:     time.Now().Truncate(time.Second),
		Fr:        createParty(from),
		To:        createParty(to),
	}
}

func createParty(bic string) Party {
	p := Party{}
	p.FIId.FinInstnId.BICFI = bic
	return p
}
