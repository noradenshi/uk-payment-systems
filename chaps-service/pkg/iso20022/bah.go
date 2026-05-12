package iso20022

import "time"

// BusinessMessage is the top-level wrapper
type BusinessMessage struct {
	AppHdr   AppHdr   `xml:"AppHdr"`
	Document any      `xml:"Document"` // Can hold pacs.008, pacs.002, etc.
}

type AppHdr struct {
	Xmlns      string    `xml:"xmlns,attr"`
	Fr         Party     `xml:"Fr"`         // Sender
	To         Party     `xml:"To"`         // Receiver
	BizMsgIdr  string    `xml:"BizMsgIdr"`  // Unique ID for this specific transmission
	MsgDefIdr  string    `xml:"MsgDefIdr"`  // e.g., "pacs.008.001.14"
	CreDt      time.Time `xml:"CreDt"`      // Creation Date
	Sgntr      *Signature `xml:"Sgntr,omitempty"` 
}

type Party struct {
	FIId struct {
		FinInstnId struct {
			BICFI string `xml:"BICFI"`
		} `xml:"FinInstnId"`
	} `xml:"FIId"`
}

type Signature struct {
    // This is where XMLDSig would go later
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
