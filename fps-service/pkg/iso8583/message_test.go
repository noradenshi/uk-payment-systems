package iso8583

import (
	"testing"
)

func build0200(withAmount bool) []byte {
	var buf []byte
	buf = append(buf, []byte("0200")...)
	primaryBmp := uint64(0)
	secondaryBmp := uint64(0)
	setBit := func(pos int) {
		if pos <= 64 {
			primaryBmp |= 1 << (64 - pos)
		} else {
			if pos > 128 {
				return
			}
			primaryBmp |= 1 << 63
			secondaryBmp |= 1 << (128 - pos)
		}
	}
	setBit(32)
	setBit(100)
	if withAmount {
		setBit(4)
		setBit(11)
	}
	primBytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		primBytes[7-i] = byte((primaryBmp >> (i * 8)) & 0xFF)
	}
	buf = append(buf, primBytes...)
	if (primaryBmp & (1 << 63)) != 0 {
		secBytes := make([]byte, 8)
		for i := 7; i >= 0; i-- {
			secBytes[7-i] = byte((secondaryBmp >> (i * 8)) & 0xFF)
		}
		buf = append(buf, secBytes...)
	}
	if withAmount {
		buf = append(buf, []byte("000000005000")...)
		buf = append(buf, []byte("123456")...)
	}
	buf = append(buf, byte(8))
	buf = append(buf, []byte("SNDRUK22")...)
	buf = append(buf, byte(8))
	buf = append(buf, []byte("BARCGB2L")...)
	return buf
}

func TestParseISO8583_ShortMessage(t *testing.T) {
	_, err := ParseISO8583([]byte{0x30, 0x30})
	if err == nil {
		t.Fatal("expected error for short message")
	}
}

func TestParseISO8583_WrongMTI(t *testing.T) {
	raw := build0200(false)
	raw[0] = '0'
	raw[1] = '1'
	raw[2] = '0'
	raw[3] = '0'
	_, err := ParseISO8583(raw)
	if err == nil {
		t.Fatal("expected error for wrong MTI")
	}
}

func TestParseISO8583_NoOptionalFields(t *testing.T) {
	raw := build0200(false)
	m, err := ParseISO8583(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.MTI != "0200" {
		t.Fatalf("expected MTI 0200, got %s", m.MTI)
	}
	if m.DE32_Acquirer != "SNDRUK22" {
		t.Fatalf("expected acquirer SNDRUK22, got %s", m.DE32_Acquirer)
	}
	if m.DE100_Receiver != "BARCGB2L" {
		t.Fatalf("expected receiver BARCGB2L, got %s", m.DE100_Receiver)
	}
	if m.DE4_Amount != 0 {
		t.Fatalf("expected amount 0 (absent), got %d", m.DE4_Amount)
	}
}

func TestParseISO8583_WithAmountAndTrace(t *testing.T) {
	raw := build0200(true)
	m, err := ParseISO8583(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.DE4_Amount != 5000 {
		t.Fatalf("expected amount 5000 (50.00), got %d", m.DE4_Amount)
	}
	if m.DE11_Trace != 123456 {
		t.Fatalf("expected trace 123456, got %d", m.DE11_Trace)
	}
}

func TestMessage0210_Encode(t *testing.T) {
	m := &Message0210{
		DE39_RespCode: "00",
		DE4_Amount:    5000,
		DE11_Trace:    123456,
		DE32_Acquirer: "SNDRUK22",
		DE100_Receiver: "BARCGB2L",
	}
	raw := m.Encode()
	if len(raw) == 0 {
		t.Fatal("empty encode result")
	}
	if string(raw[0:4]) != "0210" {
		t.Fatalf("expected MTI 0210, got %s", string(raw[0:4]))
	}
}

func TestRoundTrip(t *testing.T) {
	raw := build0200(true)
	m, err := ParseISO8583(raw)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	resp := &Message0210{
		DE39_RespCode: "00",
		DE4_Amount:    m.DE4_Amount,
		DE11_Trace:    m.DE11_Trace,
		DE32_Acquirer: m.DE32_Acquirer,
		DE100_Receiver: m.DE100_Receiver,
	}
	encoded := resp.Encode()
	if string(encoded[0:4]) != "0210" {
		t.Fatalf("expected MTI 0210, got %s", string(encoded[0:4]))
	}
}

func TestParseFullFields(t *testing.T) {
	var buf []byte
	buf = append(buf, []byte("0200")...)
	primBmp := uint64(0)
	secBmp := uint64(0)
	for _, bit := range []int{2, 3, 4, 7, 11, 32, 37, 41, 49, 100} {
		if bit <= 64 {
			primBmp |= 1 << (64 - bit)
		} else {
			primBmp |= 1 << 63
			secBmp |= 1 << (128 - bit)
		}
	}
	for i := 7; i >= 0; i-- {
		buf = append(buf, byte((primBmp >> (i * 8)) & 0xFF))
	}
	secBytes := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		secBytes[7-i] = byte((secBmp >> (i * 8)) & 0xFF)
	}
	buf = append(buf, secBytes...)
	buf = append(buf, byte(16))
	buf = append(buf, []byte("1234567890123456")...)
	buf = append(buf, []byte("100000")...)
	buf = append(buf, []byte("000000005000")...)
	buf = append(buf, []byte("0518142530")...)
	buf = append(buf, []byte("654321")...)
	buf = append(buf, byte(8))
	buf = append(buf, []byte("SNDRUK22")...)
	buf = append(buf, []byte("REF123456789")...)
	buf = append(buf, []byte("FPSGW01 ")...)
	buf = append(buf, []byte("826")...)
	buf = append(buf, byte(8))
	buf = append(buf, []byte("BARCGB2L")...)

	m, err := ParseISO8583(buf)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if m.DE2_PAN != "1234567890123456" {
		t.Fatalf("DE2: expected 1234567890123456, got %s", m.DE2_PAN)
	}
	if m.DE3_ProcCode != "100000" {
		t.Fatalf("DE3: expected 100000, got %s", m.DE3_ProcCode)
	}
	if m.DE4_Amount != 5000 {
		t.Fatalf("DE4: expected 5000, got %d", m.DE4_Amount)
	}
	if m.DE7_TransDateTime != "0518142530" {
		t.Fatalf("DE7: expected 0518142530, got %s", m.DE7_TransDateTime)
	}
	if m.DE11_Trace != 654321 {
		t.Fatalf("DE11: expected 654321, got %d", m.DE11_Trace)
	}
	if m.DE32_Acquirer != "SNDRUK22" {
		t.Fatalf("DE32: expected SNDRUK22, got %s", m.DE32_Acquirer)
	}
	if m.DE37_RefNum != "REF123456789" {
		t.Fatalf("DE37: expected REF123456789, got %s", m.DE37_RefNum)
	}
	if m.DE49_Currency != "826" {
		t.Fatalf("DE49: expected 826, got %s", m.DE49_Currency)
	}
	if m.DE100_Receiver != "BARCGB2L" {
		t.Fatalf("DE100: expected BARCGB2L, got %s", m.DE100_Receiver)
	}
}
