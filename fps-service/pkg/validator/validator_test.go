package validator_test

import (
	"testing"

	"fps-service/pkg/validator"
)

const xsdDir = "../../xsd/"

func xsdPath(name string) string {
	return xsdDir + name + ".xsd"
}

func TestNewValidatorRegistry(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	if reg == nil {
		t.Fatal("expected non-nil registry")
	}
}

func TestRegister_Success(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	err := reg.Register("pacs.008.001.14", xsdPath("pacs.008.001.14"))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestRegister_InvalidPath(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	err := reg.Register("nonexistent", "/invalid/path.xsd")
	if err == nil {
		t.Fatal("expected error for non-existent XSD path")
	}
}

func TestValidateByVersion_ValidXML(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	if err := reg.Register("pacs.008.001.14", xsdPath("pacs.008.001.14")); err != nil {
		t.Fatal(err)
	}
	xml := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
    <FIToFICstmrCdtTrf>
        <GrpHdr>
            <MsgId>TEST-001</MsgId>
            <CreDtTm>2026-06-03T12:00:00Z</CreDtTm>
            <NbOfTxs>1</NbOfTxs>
            <SttlmInf><SttlmMtd>CLRG</SttlmMtd></SttlmInf>
        </GrpHdr>
        <CdtTrfTxInf>
            <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
            <IntrBkSttlmAmt Ccy="GBP">100.00</IntrBkSttlmAmt>
            <ChrgBr>SLEV</ChrgBr>
            <Dbtr><Nm>Sender Name</Nm></Dbtr>
            <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
            <CdtrAgt><FinInstnId><BICFI>HSBCGB44</BICFI></FinInstnId></CdtrAgt>
            <Cdtr><Nm>Receiver Name</Nm></Cdtr>
        </CdtTrfTxInf>
    </FIToFICstmrCdtTrf>
</Document>`)
	if err := reg.ValidateByVersion("pacs.008.001.14", xml); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateByVersion_InvalidXML(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	if err := reg.Register("pacs.008.001.14", xsdPath("pacs.008.001.14")); err != nil {
		t.Fatal(err)
	}
	xml := []byte(`<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
    <FIToFICstmrCdtTrf>
        <GrpHdr>
            <MsgId>TEST-001</MsgId>
        </GrpHdr>
    </FIToFICstmrCdtTrf>
</Document>`)
	err := reg.ValidateByVersion("pacs.008.001.14", xml)
	if err == nil {
		t.Fatal("expected validation error for incomplete XML")
	}
}

func TestValidateByVersion_UnregisteredVersion(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	xml := []byte(`<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14"/>`)
	err := reg.ValidateByVersion("pacs.999.999.99", xml)
	if err == nil {
		t.Fatal("expected error for unregistered version")
	}
}

func TestValidateByVersion_MalformedXML(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	if err := reg.Register("pacs.008.001.14", xsdPath("pacs.008.001.14")); err != nil {
		t.Fatal(err)
	}
	xml := []byte(`<Document><unclosed>`)
	err := reg.ValidateByVersion("pacs.008.001.14", xml)
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}

func TestValidateWrapped_ValidEnvelope(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	for _, name := range []string{"chaps_wrapper", "pacs.008.001.14", "pacs.002.001.16", "head.001.001.02", "head.001.001.04"} {
		if err := reg.Register(name, xsdPath(name)); err != nil {
			t.Fatal(err)
		}
	}

	payload := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<BizMsg>
    <AppHdr xmlns="urn:iso:std:iso:20022:tech:xsd:head.001.001.02">
        <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
        <To><FIId><FinInstnId><BICFI>RCVRUK22</BICFI></FinInstnId></FIId></To>
        <BizMsgIdr>BAH-TEST-001</BizMsgIdr>
        <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
        <CreDt>2026-06-03T12:00:00Z</CreDt>
    </AppHdr>
    <Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
        <FIToFICstmrCdtTrf>
            <GrpHdr>
                <MsgId>TEST-001</MsgId>
                <CreDtTm>2026-06-03T12:00:00Z</CreDtTm>
                <NbOfTxs>1</NbOfTxs>
                <SttlmInf><SttlmMtd>CLRG</SttlmMtd></SttlmInf>
            </GrpHdr>
            <CdtTrfTxInf>
                <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
                <IntrBkSttlmAmt Ccy="GBP">100.00</IntrBkSttlmAmt>
                <ChrgBr>SLEV</ChrgBr>
                <Dbtr><Nm>Sender Name</Nm></Dbtr>
                <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
                <CdtrAgt><FinInstnId><BICFI>HSBCGB44</BICFI></FinInstnId></CdtrAgt>
                <Cdtr><Nm>Receiver Name</Nm></Cdtr>
            </CdtTrfTxInf>
        </FIToFICstmrCdtTrf>
    </Document>
</BizMsg>`)

	docBytes, version, err := reg.ValidateWrapped(payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if version != "pacs.008.001.14" {
		t.Fatalf("expected version pacs.008.001.14, got: %s", version)
	}
	if len(docBytes) == 0 {
		t.Fatal("expected non-empty document bytes")
	}
}

func TestValidateWrapped_MissingAppHdr(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	for _, name := range []string{"chaps_wrapper", "pacs.008.001.14", "pacs.002.001.16", "head.001.001.02", "head.001.001.04"} {
		if err := reg.Register(name, xsdPath(name)); err != nil {
			t.Fatal(err)
		}
	}

	payload := []byte(`<BizMsg>
    <Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
        <FIToFICstmrCdtTrf>
            <GrpHdr>
                <MsgId>TEST-001</MsgId>
                <CreDtTm>2026-06-03T12:00:00Z</CreDtTm>
                <NbOfTxs>1</NbOfTxs>
                <SttlmInf><SttlmMtd>CLRG</SttlmMtd></SttlmInf>
            </GrpHdr>
            <CdtTrfTxInf>
                <PmtId><EndToEndId>E2E-001</EndToEndId></PmtId>
                <IntrBkSttlmAmt Ccy="GBP">100.00</IntrBkSttlmAmt>
                <ChrgBr>SLEV</ChrgBr>
                <Dbtr><Nm>Sender Name</Nm></Dbtr>
                <DbtrAgt><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></DbtrAgt>
                <CdtrAgt><FinInstnId><BICFI>HSBCGB44</BICFI></FinInstnId></CdtrAgt>
                <Cdtr><Nm>Receiver Name</Nm></Cdtr>
            </CdtTrfTxInf>
        </FIToFICstmrCdtTrf>
    </Document>
</BizMsg>`)

	_, _, err := reg.ValidateWrapped(payload)
	if err == nil {
		t.Fatal("expected validation error for envelope missing AppHdr")
	}
}

func TestValidateWrapped_InvalidDocumentContent(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	for _, name := range []string{"chaps_wrapper", "pacs.008.001.14", "pacs.002.001.16", "head.001.001.02", "head.001.001.04"} {
		if err := reg.Register(name, xsdPath(name)); err != nil {
			t.Fatal(err)
		}
	}

	payload := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<BizMsg>
    <AppHdr xmlns="urn:iso:std:iso:20022:tech:xsd:head.001.001.02">
        <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
        <To><FIId><FinInstnId><BICFI>RCVRUK22</BICFI></FinInstnId></FIId></To>
        <BizMsgIdr>BAH-TEST-001</BizMsgIdr>
        <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
        <CreDt>2026-06-03T12:00:00Z</CreDt>
    </AppHdr>
    <Document xmlns="urn:iso:std:iso:20022:tech:xsd:pacs.008.001.14">
        <UnknownRoot>data</UnknownRoot>
    </Document>
</BizMsg>`)

	_, _, err := reg.ValidateWrapped(payload)
	if err == nil {
		t.Fatal("expected validation error for incomplete Document")
	}
}

func TestValidateWrapped_UnregisteredDocumentSchema(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	if err := reg.Register("chaps_wrapper", xsdPath("chaps_wrapper")); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<BizMsg>
    <AppHdr xmlns="urn:iso:std:iso:20022:tech:xsd:head.001.001.02">
        <Fr><FIId><FinInstnId><BICFI>SNDRUK22</BICFI></FinInstnId></FIId></Fr>
        <To><FIId><FinInstnId><BICFI>RCVRUK22</BICFI></FinInstnId></FIId></To>
        <BizMsgIdr>BAH-TEST-001</BizMsgIdr>
        <MsgDefIdr>pacs.008.001.14</MsgDefIdr>
        <CreDt>2026-06-03T12:00:00Z</CreDt>
    </AppHdr>
    <MyCustom xmlns="urn:unknown:schema">data</MyCustom>
</BizMsg>`)

	_, _, err := reg.ValidateWrapped(payload)
	if err == nil {
		t.Fatal("expected validation error for unregistered Document namespace")
	}
}

func TestValidateWrapped_MalformedXML(t *testing.T) {
	reg := validator.NewValidatorRegistry()
	for _, name := range []string{"chaps_wrapper", "pacs.008.001.14", "pacs.002.001.16", "head.001.001.02", "head.001.001.04"} {
		if err := reg.Register(name, xsdPath(name)); err != nil {
			t.Fatal(err)
		}
	}

	payload := []byte(`<BizMsg><unclosed>`)
	_, _, err := reg.ValidateWrapped(payload)
	if err == nil {
		t.Fatal("expected error for malformed XML")
	}
}
