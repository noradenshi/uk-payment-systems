package iso20022_test

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"chaps-service/pkg/iso20022"
)

func TestPacs008Unmarshal(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
  <FIToFICstmrCdtTrf>
    <GrpHdr>
      <MsgId>CHAPS-TEST-001</MsgId>
    </GrpHdr>
    <CdtTrfTxInf>
      <PmtId>
        <EndToEndId>E2E-1001</EndToEndId>
      </PmtId>
      <IntrBkSttlmAmt Ccy="GBP">250.50</IntrBkSttlmAmt>
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
    </CdtTrfTxInf>
  </FIToFICstmrCdtTrf>
</Document>`

	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal([]byte(xmlData), &msg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if msg.MsgId != "CHAPS-TEST-001" {
		t.Errorf("MsgId = %q, want %q", msg.MsgId, "CHAPS-TEST-001")
	}
	if msg.EndToEndId != "E2E-1001" {
		t.Errorf("EndToEndId = %q, want %q", msg.EndToEndId, "E2E-1001")
	}
	if msg.Sender != "SNDRUK22" {
		t.Errorf("Sender = %q, want %q", msg.Sender, "SNDRUK22")
	}
	if msg.DestBIC != "HSBCGB44" {
		t.Errorf("DestBIC = %q, want %q", msg.DestBIC, "HSBCGB44")
	}
	if msg.Amount != 250.50 {
		t.Errorf("Amount = %f, want %f", msg.Amount, 250.50)
	}
}

func TestPacs008Unmarshal_EmptyFields(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<Document>
  <FIToFICstmrCdtTrf>
    <GrpHdr>
      <MsgId></MsgId>
    </GrpHdr>
    <CdtTrfTxInf>
      <PmtId>
        <EndToEndId></EndToEndId>
      </PmtId>
      <IntrBkSttlmAmt>0</IntrBkSttlmAmt>
      <DbtrAgt>
        <FinInstnId>
          <BICFI></BICFI>
        </FinInstnId>
      </DbtrAgt>
      <CdtrAgt>
        <FinInstnId>
          <BICFI></BICFI>
        </FinInstnId>
      </CdtrAgt>
    </CdtTrfTxInf>
  </FIToFICstmrCdtTrf>
</Document>`

	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal([]byte(xmlData), &msg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if msg.MsgId != "" {
		t.Errorf("expected empty MsgId, got %q", msg.MsgId)
	}
	if msg.Amount != 0 {
		t.Errorf("expected zero Amount, got %f", msg.Amount)
	}
}

func TestPacs002Marshal(t *testing.T) {
	msg := iso20022.NewPacs002("CHAPS-ORIG-001", "E2E-1001", "ACTC", "SNDRUK22", "HSBCGB44", "")

	data, err := xml.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "ACTC") {
		t.Errorf("expected status ACTC in output, got: %s", s)
	}
	if !strings.Contains(s, "SNDRUK22") {
		t.Errorf("expected sender BIC in output")
	}
	if !strings.Contains(s, "HSBCGB44") {
		t.Errorf("expected receiver BIC in output")
	}
	if !strings.Contains(s, "pacs.002.001.16") {
		t.Errorf("expected namespace in output")
	}
}

func TestPacs002Marshal_Rejected(t *testing.T) {
	msg := iso20022.NewPacs002("CHAPS-ORIG-002", "E2E-1002", "RJCT", "SNDRUK22", "HSBCGB44", "NARRATIVE")

	data, err := xml.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "RJCT") {
		t.Errorf("expected status RJCT in output")
	}
	if !strings.Contains(s, "NARRATIVE") {
		t.Errorf("expected reason code NARRATIVE in output")
	}
}

func TestPacs002Marshal_Pending(t *testing.T) {
	msg := iso20022.NewPacs002("CHAPS-ORIG-003", "E2E-1003", "PDNG", "SNDRUK22", "HSBCGB44", "")

	data, err := xml.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "PDNG") {
		t.Errorf("expected status PDNG in output, got: %s", s)
	}
}

func TestNewPacs002_SetsTimestamps(t *testing.T) {
	before := time.Now().Truncate(time.Second)
	msg := iso20022.NewPacs002("CHAPS-ORIG-004", "E2E-1004", "ACTC", "SNDRUK22", "HSBCGB44", "")
	after := time.Now().Truncate(time.Second)

	if msg.PmtStsRpt.GrpHdr.CreDtTm.Before(before) || msg.PmtStsRpt.GrpHdr.CreDtTm.After(after) {
		t.Errorf("timestamp out of range: %v (between %v and %v)", msg.PmtStsRpt.GrpHdr.CreDtTm, before, after)
	}
}

func TestBusinessMessageEnvelope(t *testing.T) {
	pacs002 := iso20022.NewPacs002("CHAPS-ORIG-005", "E2E-1005", "ACTC", "SNDRUK22", "HSBCGB44", "")
	env := iso20022.BusinessMessage{
		AppHdr:   iso20022.NewBAH("HSBCGB44", "SNDRUK22", "CHAPS-ORIG-005", "pacs.002.001.16"),
		Document: pacs002,
	}

	data, err := xml.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, "BizMsg") {
		t.Errorf("expected BizMsg root element, got: %s", s)
	}
	if !strings.Contains(s, "AppHdr") {
		t.Errorf("expected AppHdr element")
	}
	if !strings.Contains(s, "Document") {
		t.Errorf("expected Document element")
	}
	if !strings.Contains(s, "ACTC") {
		t.Errorf("expected status ACTC")
	}
	if !strings.Contains(s, "head.001.001.04") {
		t.Errorf("expected head namespace")
	}
}

func TestBAH(t *testing.T) {
	ah := iso20022.NewBAH("SNDRUK22", "HSBCGB44", "MSG-001", "pacs.002.001.16")

	if ah.BizMsgIdr != "BAH-MSG-001" {
		t.Errorf("BizMsgIdr = %q, want %q", ah.BizMsgIdr, "BAH-MSG-001")
	}
	if ah.MsgDefIdr != "pacs.002.001.16" {
		t.Errorf("MsgDefIdr = %q, want %q", ah.MsgDefIdr, "pacs.002.001.16")
	}
	if ah.Fr.FIId.FinInstnId.BICFI != "SNDRUK22" {
		t.Errorf("Fr BIC = %q, want %q", ah.Fr.FIId.FinInstnId.BICFI, "SNDRUK22")
	}
	if ah.To.FIId.FinInstnId.BICFI != "HSBCGB44" {
		t.Errorf("To BIC = %q, want %q", ah.To.FIId.FinInstnId.BICFI, "HSBCGB44")
	}
}

func TestNewPacs002_DefaultReasonNil(t *testing.T) {
	msg := iso20022.NewPacs002("CHAPS-ORIG-006", "E2E-1006", "ACTC", "SNDRUK22", "HSBCGB44", "")
	if msg.PmtStsRpt.TxInfAndSts.StsRsnInf != nil {
		t.Errorf("expected nil StsRsnInf for empty reason")
	}
}

func TestNewPacs002_WithReason(t *testing.T) {
	msg := iso20022.NewPacs002("CHAPS-ORIG-007", "E2E-1007", "RJCT", "SNDRUK22", "HSBCGB44", "INSUFFICIENT-FUNDS")
	if msg.PmtStsRpt.TxInfAndSts.StsRsnInf == nil {
		t.Fatal("expected non-nil StsRsnInf")
	}
	if msg.PmtStsRpt.TxInfAndSts.StsRsnInf.Rsn.Cd != "INSUFFICIENT-FUNDS" {
		t.Errorf("reason code = %q, want %q", msg.PmtStsRpt.TxInfAndSts.StsRsnInf.Rsn.Cd, "INSUFFICIENT-FUNDS")
	}
}

func TestRoundTrip_Pacs008ThroughEnvelope(t *testing.T) {
	pacs008 := &iso20022.Pacs008Message{
		MsgId:      "ROUND-TRIP-001",
		EndToEndId: "E2E-ROUND",
		Sender:     "SNDRUK22",
		DestBIC:    "HSBCGB44",
		Amount:     999.99,
	}

	env := iso20022.BusinessMessage{
		AppHdr:   iso20022.NewBAH("SNDRUK22", "HSBCGB44", "ROUND-TRIP-001", "pacs.008.001.14"),
		Document: pacs008,
	}

	data, err := xml.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded iso20022.BusinessMessage
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.AppHdr.Fr.FIId.FinInstnId.BICFI != "SNDRUK22" {
		t.Errorf("Fr BIC = %q, want %q", decoded.AppHdr.Fr.FIId.FinInstnId.BICFI, "SNDRUK22")
	}
	if decoded.AppHdr.To.FIId.FinInstnId.BICFI != "HSBCGB44" {
		t.Errorf("To BIC = %q, want %q", decoded.AppHdr.To.FIId.FinInstnId.BICFI, "HSBCGB44")
	}
}

func TestPacs008_AmountWithCurrency(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<Document>
  <FIToFICstmrCdtTrf>
    <GrpHdr>
      <MsgId>PAY-CURRENCY-001</MsgId>
    </GrpHdr>
    <CdtTrfTxInf>
      <PmtId>
        <EndToEndId>E2E-CURR</EndToEndId>
      </PmtId>
      <IntrBkSttlmAmt Ccy="GBP">99999999.99</IntrBkSttlmAmt>
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
    </CdtTrfTxInf>
  </FIToFICstmrCdtTrf>
</Document>`

	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal([]byte(xmlData), &msg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if msg.MsgId != "PAY-CURRENCY-001" {
		t.Errorf("MsgId = %q", msg.MsgId)
	}
	if msg.Amount != 99999999.99 {
		t.Errorf("Amount = %f, want 99999999.99", msg.Amount)
	}
}

func TestFullPacs008FromTestFile(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
    <FIToFICstmrCdtTrf>
        <GrpHdr>
            <MsgId>CHAPS-TEST-001</MsgId>
            <CreDtTm>2026-04-29T13:55:00Z</CreDtTm>
            <NbOfTxs>1</NbOfTxs>
            <SttlmInf>
                <SttlmMtd>CLRG</SttlmMtd>
            </SttlmInf>
        </GrpHdr>
        <CdtTrfTxInf>
            <PmtId>
                <EndToEndId>ORDER-999</EndToEndId>
            </PmtId>
            <IntrBkSttlmAmt Ccy="GBP">100.00</IntrBkSttlmAmt>
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
</Document>`

	var msg iso20022.Pacs008Message
	if err := xml.Unmarshal([]byte(xmlData), &msg); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if msg.MsgId != "CHAPS-TEST-001" {
		t.Errorf("MsgId = %q, want %q", msg.MsgId, "CHAPS-TEST-001")
	}
	if msg.EndToEndId != "ORDER-999" {
		t.Errorf("EndToEndId = %q, want %q", msg.EndToEndId, "ORDER-999")
	}
	if msg.Sender != "SNDRUK22" {
		t.Errorf("Sender = %q, want %q", msg.Sender, "SNDRUK22")
	}
	if msg.DestBIC != "HSBCGB44" {
		t.Errorf("DestBIC = %q, want %q", msg.DestBIC, "HSBCGB44")
	}
	if msg.Amount != 100.00 {
		t.Errorf("Amount = %f, want %f", msg.Amount, 100.00)
	}
}
