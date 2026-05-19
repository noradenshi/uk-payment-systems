package iso8583

import (
	"encoding/binary"
	"fmt"
	"strconv"
)

type Message0200 struct {
	MTI               string
	DE2_PAN           string
	DE3_ProcCode      string
	DE4_Amount        int64
	DE7_TransDateTime string
	DE11_Trace        int
	DE32_Acquirer     string
	DE37_RefNum       string
	DE41_TerminalID   string
	DE49_Currency     string
	DE100_Receiver    string
}

func parseNumeric(raw []byte, length int) string {
	if len(raw) < length {
		return string(raw)
	}
	return string(raw[:length])
}

func parseLLVAR(raw []byte, off *int) (string, error) {
	if *off+1 > len(raw) {
		return "", fmt.Errorf("offset %d exceeds data length %d", *off, len(raw))
	}
	length := int(raw[*off])
	*off++
	if *off+length > len(raw) {
		return "", fmt.Errorf("LLVAR length %d exceeds data at offset %d", length, *off)
	}
	val := string(raw[*off : *off+length])
	*off += length
	return val, nil
}

func parseN6(raw []byte, off *int) (string, error) {
	if *off+6 > len(raw) {
		return "", fmt.Errorf("N6 at offset %d exceeds data", *off)
	}
	val := string(raw[*off : *off+6])
	*off += 6
	return val, nil
}

func parseN12(raw []byte, off *int) (int64, error) {
	if *off+12 > len(raw) {
		return 0, fmt.Errorf("N12 at offset %d exceeds data", *off)
	}
	val, err := strconv.ParseInt(string(raw[*off:*off+12]), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("N12 parse error at offset %d: %w", *off, err)
	}
	*off += 12
	return val, nil
}

func parseN10(raw []byte, off *int) (string, error) {
	if *off+10 > len(raw) {
		return "", fmt.Errorf("N10 at offset %d exceeds data", *off)
	}
	val := string(raw[*off : *off+10])
	*off += 10
	return val, nil
}

func parseAN(num int) func([]byte, *int) (string, error) {
	return func(raw []byte, off *int) (string, error) {
		if *off+num > len(raw) {
			return "", fmt.Errorf("AN%d at offset %d exceeds data", num, *off)
		}
		val := string(raw[*off : *off+num])
		*off += num
		return val, nil
	}
}

type fieldDef struct {
	bit   int
	parse func([]byte, *int) (interface{}, error)
	set   func(*Message0200, interface{})
}

var fieldMap = []fieldDef{
	{2, func(raw []byte, off *int) (interface{}, error) { return parseLLVAR(raw, off) }, func(m *Message0200, v interface{}) { m.DE2_PAN, _ = v.(string) }},
	{3, func(raw []byte, off *int) (interface{}, error) { return parseN6(raw, off) }, func(m *Message0200, v interface{}) { m.DE3_ProcCode, _ = v.(string) }},
	{4, func(raw []byte, off *int) (interface{}, error) { return parseN12(raw, off) }, func(m *Message0200, v interface{}) { m.DE4_Amount, _ = v.(int64) }},
	{7, func(raw []byte, off *int) (interface{}, error) { return parseN10(raw, off) }, func(m *Message0200, v interface{}) { m.DE7_TransDateTime, _ = v.(string) }},
	{11, func(raw []byte, off *int) (interface{}, error) { return parseN6(raw, off) }, func(m *Message0200, v interface{}) { m.DE11_Trace, _ = strconv.Atoi(v.(string)) }},
	{32, func(raw []byte, off *int) (interface{}, error) { return parseLLVAR(raw, off) }, func(m *Message0200, v interface{}) { m.DE32_Acquirer, _ = v.(string) }},
	{37, func(raw []byte, off *int) (interface{}, error) { return parseAN(12)(raw, off) }, func(m *Message0200, v interface{}) { m.DE37_RefNum, _ = v.(string) }},
	{41, func(raw []byte, off *int) (interface{}, error) { return parseAN(8)(raw, off) }, func(m *Message0200, v interface{}) { m.DE41_TerminalID, _ = v.(string) }},
	{49, func(raw []byte, off *int) (interface{}, error) { return parseAN(3)(raw, off) }, func(m *Message0200, v interface{}) { m.DE49_Currency, _ = v.(string) }},
	{100, func(raw []byte, off *int) (interface{}, error) { return parseLLVAR(raw, off) }, func(m *Message0200, v interface{}) { m.DE100_Receiver, _ = v.(string) }},
}

var fieldLookup = map[int]fieldDef{}
var fieldBits []int

func init() {
	for _, f := range fieldMap {
		fieldLookup[f.bit] = f
		fieldBits = append(fieldBits, f.bit)
	}
}

func ParseISO8583(raw []byte) (*Message0200, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("message too short: %d bytes", len(raw))
	}
	m := &Message0200{
		MTI: string(raw[0:4]),
	}
	if m.MTI != "0200" {
		return nil, fmt.Errorf("unsupported MTI: %s", m.MTI)
	}

	off := 4
	if len(raw) < off+8 {
		return nil, fmt.Errorf("missing primary bitmap")
	}
	primBitmap := binary.BigEndian.Uint64(raw[off : off+8])
	off += 8

	hasSecondary := (primBitmap & (1 << 63)) != 0
	var secBitmap uint64
	if hasSecondary {
		if len(raw) < off+8 {
			return nil, fmt.Errorf("missing secondary bitmap")
		}
		secBitmap = binary.BigEndian.Uint64(raw[off : off+8])
		off += 8
	}

	for _, bit := range fieldBits {
		var present bool
		if bit <= 64 {
			present = (primBitmap & (1 << (64 - bit))) != 0
		} else if hasSecondary && bit <= 128 {
			present = (secBitmap & (1 << (128 - bit))) != 0
		}
		if !present {
			continue
		}

		fd, ok := fieldLookup[bit]
		if !ok {
			continue
		}
		val, err := fd.parse(raw, &off)
		if err != nil {
			return nil, fmt.Errorf("DE%d parse error at offset %d: %w", bit, off, err)
		}
		fd.set(m, val)
	}

	return m, nil
}

type Message0210 struct {
	MTI           string
	DE39_RespCode string
	DE4_Amount    int64
	DE11_Trace    int
	DE32_Acquirer string
	DE100_Receiver string
}

func (m *Message0210) Encode() []byte {
	var buf []byte
	buf = append(buf, []byte("0210")...)

	primaryBmp := uint64(0)
	secondaryBmp := uint64(0)
	setBit := func(pos int) {
		if pos <= 64 {
			primaryBmp |= 1 << (64 - pos)
		} else if pos <= 128 {
			primaryBmp |= 1 << 63
			secondaryBmp |= 1 << (128 - pos)
		}
	}
	setBit(4)
	setBit(11)
	setBit(32)
	setBit(39)
	setBit(100)

	primBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(primBytes, primaryBmp)
	buf = append(buf, primBytes...)
	if secondaryBmp != 0 {
		secBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(secBytes, secondaryBmp)
		buf = append(buf, secBytes...)
	}

	amtStr := fmt.Sprintf("%012d", m.DE4_Amount)
	buf = append(buf, []byte(amtStr)...)

	traceStr := fmt.Sprintf("%06d", m.DE11_Trace)
	buf = append(buf, []byte(traceStr)...)

	buf = append(buf, byte(len(m.DE32_Acquirer)))
	buf = append(buf, []byte(m.DE32_Acquirer)...)

	buf = append(buf, byte(len(m.DE39_RespCode)))
	buf = append(buf, []byte(m.DE39_RespCode)...)

	buf = append(buf, byte(len(m.DE100_Receiver)))
	buf = append(buf, []byte(m.DE100_Receiver)...)

	return buf
}
