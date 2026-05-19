package standard18_test

import (
	"testing"

	"bacs-service/pkg/standard18"
)

func str(r byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = r
	}
	return string(b)
}

func rpad(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + str(' ', n-len(s))
}

func lpad(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return str(' ', n-len(s)) + s
}

func rec1(volNo, sortCode, account string, totalValuePence int64, totalVol int, date string) string {
	return "1" +
		lpad(volNo, 7) +
		rpad(sortCode, 9) +
		rpad(account, 9) +
		str(' ', 29) +
		lpad(formatPence(totalValuePence), 11) +
		lpad(formatInt(totalVol), 7) +
		rpad(date, 6) +
		" "
}

func rec3(volNo, sortCode, account string, amtPence int64, orig, transType, ref, suCode string) string {
	return "3" +
		lpad(volNo, 7) +
		rpad(sortCode, 9) +
		rpad(account, 9) +
		lpad(formatPence(amtPence), 11) +
		rpad(orig, 15) +
		rpad(transType, 1) +
		rpad(ref, 14) +
		rpad(suCode, 12) +
		" "
}

func rec4(volNo, sortCode, account string, amtPence int64, origName, payrollRef, suCode string) string {
	return "4" +
		lpad(volNo, 7) +
		rpad(sortCode, 9) +
		rpad(account, 9) +
		lpad(formatPence(amtPence), 11) +
		rpad(origName, 15) +
		rpad(payrollRef, 14) +
		rpad(suCode, 13) +
		" "
}

func rec5(volNo string, recCount int) string {
	return "5" +
		lpad(volNo, 7) +
		str(' ', 40) +
		lpad(formatInt(recCount), 8) +
		str(' ', 24)
}

func rec9(volNo string, totalValuePence int64, totalVol, hashTotal int) string {
	return "9" +
		lpad(volNo, 7) +
		str(' ', 12) +
		lpad(formatPence(totalValuePence), 11) +
		lpad(formatInt(totalVol), 9) +
		lpad(formatInt(hashTotal), 14) +
		str(' ', 26)
}

func recA(volNo, instruction, sortCode, account, ref string, amtPence int64) string {
	return "A" +
		lpad(volNo, 7) +
		rpad(instruction, 1) +
		rpad(sortCode, 9) +
		rpad(account, 9) +
		rpad(ref, 18) +
		str(' ', 8) +
		lpad(formatPence(amtPence), 12) +
		str(' ', 15)
}

func formatPence(v int64) string {
	s := ""
	for i := v; i > 0; i /= 10 {
		s = string(byte('0'+i%10)) + s
	}
	if s == "" {
		s = "0"
	}
	return s
}

func formatInt(v int) string {
	s := ""
	for i := v; i > 0; i /= 10 {
		s = string(byte('0'+i%10)) + s
	}
	if s == "" {
		s = "0"
	}
	return s
}

func TestParseBasicFile(t *testing.T) {
	content := rec1("1", "654321", "01234567", 1234500, 2, "260526") + "\n" +
		rec3("1", "654321", "01234567", 10000, "ORIGINATOR", "1", "REFERENCE", "XX1") + "\n" +
		rec4("1", "654321", "01234567", 100000, "ORIGINATOR", "PAYROLL", "001") + "\n" +
		rec5("1", 2) + "\n" +
		rec9("1", 110000, 2, 30) + "\n"

	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Header == nil {
		t.Fatal("Header is nil")
	}
	if f.Header.Type != "1" {
		t.Errorf("Header.Type = %q, want %q", f.Header.Type, "1")
	}
	if f.Header.VolumeNo != 1 {
		t.Errorf("VolumeNo = %d, want 1", f.Header.VolumeNo)
	}
	if f.Header.DestSortCode != "654321" {
		t.Errorf("DestSortCode = %q, want %q", f.Header.DestSortCode, "654321")
	}
	if f.Header.DestAccount != "01234567" {
		t.Errorf("DestAccount = %q, want %q", f.Header.DestAccount, "01234567")
	}
	if f.Header.TotalValue != 12345.00 {
		t.Errorf("TotalValue = %f, want %f", f.Header.TotalValue, 12345.00)
	}
	if f.Header.TotalVolume != 2 {
		t.Errorf("TotalVolume = %d, want %d", f.Header.TotalVolume, 2)
	}
	if f.Header.Date != "260526" {
		t.Errorf("Date = %q, want %q", f.Header.Date, "260526")
	}

	if len(f.DirectDebits) != 1 {
		t.Fatalf("DirectDebits count = %d, want 1", len(f.DirectDebits))
	}
	if f.DirectDebits[0].Type != "3" {
		t.Errorf("DirectDebit[0].Type = %q, want %q", f.DirectDebits[0].Type, "3")
	}
	if f.DirectDebits[0].Amount != 100.00 {
		t.Errorf("DirectDebit[0].Amount = %f, want %f", f.DirectDebits[0].Amount, 100.00)
	}
	if f.DirectDebits[0].DestSortCode != "654321" {
		t.Errorf("DirectDebit[0].DestSortCode = %q, want %q", f.DirectDebits[0].DestSortCode, "654321")
	}
	if f.DirectDebits[0].DestAccount != "01234567" {
		t.Errorf("DirectDebit[0].DestAccount = %q, want %q", f.DirectDebits[0].DestAccount, "01234567")
	}
	if f.DirectDebits[0].OriginatorSortAcc != "ORIGINATOR" {
		t.Errorf("OriginatorSortAcc = %q, want %q", f.DirectDebits[0].OriginatorSortAcc, "ORIGINATOR")
	}
	if f.DirectDebits[0].TransType != "1" {
		t.Errorf("TransType = %q, want %q", f.DirectDebits[0].TransType, "1")
	}
	if f.DirectDebits[0].Reference != "REFERENCE" {
		t.Errorf("Reference = %q, want %q", f.DirectDebits[0].Reference, "REFERENCE")
	}
	if f.DirectDebits[0].SUCode != "XX1" {
		t.Errorf("SUCode = %q, want %q", f.DirectDebits[0].SUCode, "XX1")
	}

	if len(f.DirectCredits) != 1 {
		t.Fatalf("DirectCredits count = %d, want 1", len(f.DirectCredits))
	}
	if f.DirectCredits[0].Type != "4" {
		t.Errorf("DirectCredit[0].Type = %q, want %q", f.DirectCredits[0].Type, "4")
	}
	if f.DirectCredits[0].Amount != 1000.00 {
		t.Errorf("DirectCredit[0].Amount = %f, want %f", f.DirectCredits[0].Amount, 1000.00)
	}
	if f.DirectCredits[0].OriginatorName != "ORIGINATOR" {
		t.Errorf("OriginatorName = %q, want %q", f.DirectCredits[0].OriginatorName, "ORIGINATOR")
	}
	if f.DirectCredits[0].PayrollRef != "PAYROLL" {
		t.Errorf("PayrollRef = %q, want %q", f.DirectCredits[0].PayrollRef, "PAYROLL")
	}

	if f.Trailer5 == nil {
		t.Fatal("Trailer5 is nil")
	}
	if f.Trailer5.Type != "5" {
		t.Errorf("Trailer5.Type = %q, want %q", f.Trailer5.Type, "5")
	}
	if f.Trailer5.RecordCount != 2 {
		t.Errorf("Trailer5.RecordCount = %d, want %d", f.Trailer5.RecordCount, 2)
	}

	if f.Trailer9 == nil {
		t.Fatal("Trailer9 is nil")
	}
	if f.Trailer9.TotalValue != 1100.00 {
		t.Errorf("Trailer9.TotalValue = %f, want %f", f.Trailer9.TotalValue, 1100.00)
	}
	if f.Trailer9.TotalVolume != 2 {
		t.Errorf("Trailer9.TotalVolume = %d, want %d", f.Trailer9.TotalVolume, 2)
	}
	if f.Trailer9.HashTotal != 30 {
		t.Errorf("Trailer9.HashTotal = %d, want %d", f.Trailer9.HashTotal, 30)
	}
}

func TestParseWithAUDDIS(t *testing.T) {
	content := rec1("1", "654321", "01234567", 10000, 1, "260526") + "\n" +
		rec3("1", "654321", "01234567", 10000, "ORIGINATOR", "1", "REF", "XX1") + "\n" +
		rec5("1", 1) + "\n" +
		rec9("1", 10000, 1, 10) + "\n" +
		recA("1", "N", "654321", "01234567", "NEW MANDATE", 5000) + "\n"

	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(f.AuddisItems) != 1 {
		t.Fatalf("AUDDIS items count = %d, want 1", len(f.AuddisItems))
	}
	if f.AuddisItems[0].Type != "A" {
		t.Errorf("AUDDIS[0].Type = %q, want %q", f.AuddisItems[0].Type, "A")
	}
	if f.AuddisItems[0].Instruction != "N" {
		t.Errorf("AUDDIS[0].Instruction = %q, want %q", f.AuddisItems[0].Instruction, "N")
	}
	if f.AuddisItems[0].SortCode != "654321" {
		t.Errorf("AUDDIS[0].SortCode = %q, want %q", f.AuddisItems[0].SortCode, "654321")
	}
	if f.AuddisItems[0].Account != "01234567" {
		t.Errorf("AUDDIS[0].Account = %q, want %q", f.AuddisItems[0].Account, "01234567")
	}
	if f.AuddisItems[0].Ref != "NEW MANDATE" {
		t.Errorf("AUDDIS[0].Ref = %q, want %q", f.AuddisItems[0].Ref, "NEW MANDATE")
	}
	if f.AuddisItems[0].Amount != 50.00 {
		t.Errorf("AUDDIS[0].Amount = %f, want %f", f.AuddisItems[0].Amount, 50.00)
	}
}

func TestParseInvalidLineLength(t *testing.T) {
	content := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz\n"
	_, err := standard18.Parse(content)
	if err == nil {
		t.Fatal("expected error for long line")
	}
}

func TestParseEmptyContent(t *testing.T) {
	f, err := standard18.Parse("")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if f.Header != nil {
		t.Errorf("expected nil header for empty input")
	}
}

func TestParse_CRLF(t *testing.T) {
	content := rec1("1", "654321", "01234567", 10000, 1, "260526") + "\r\n" +
		rec9("1", 10000, 1, 15) + "\r\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if f.Header == nil {
		t.Fatal("Header is nil")
	}
	if f.Trailer9 == nil {
		t.Fatal("Trailer9 is nil")
	}
}

func TestValidate_AllPresent(t *testing.T) {
	content := rec1("1", "654321", "01234567", 10000, 1, "260526") + "\n" +
		rec3("1", "654321", "01234567", 10000, "ORIGINATOR", "1", "REF", "XX1") + "\n" +
		rec5("1", 1) + "\n" +
		rec9("1", 10000, 1, 10) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	errs := f.Validate()
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got: %v", errs)
	}
}

func TestValidate_MissingHeader(t *testing.T) {
	content := rec3("1", "654321", "01234567", 10000, "ORIGINATOR", "1", "REF", "XX1") + "\n" +
		rec5("1", 1) + "\n" +
		rec9("1", 10000, 1, 10) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	errs := f.Validate()
	found := false
	for _, e := range errs {
		if e == "Missing Record 1 (Volume Header)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Missing Record 1' error, got: %v", errs)
	}
}

func TestValidate_VolumeMismatch(t *testing.T) {
	content := rec1("1", "654321", "01234567", 10000, 2, "260526") + "\n" +
		rec9("1", 10000, 1, 10) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	errs := f.Validate()
	found := false
	for _, e := range errs {
		if e == "Missing Record 5 (Trailer Label)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Missing Record 5' error, got: %v", errs)
	}
}

func TestAmountPenceConversion(t *testing.T) {
	content := rec1("1", "654321", "01234567", 5000, 1, "260526") + "\n" +
		rec3("1", "654321", "01234567", 1999, "ORIGINATOR", "1", "REF", "XX1") + "\n" +
		rec5("1", 1) + "\n" +
		rec9("1", 1999, 1, 10) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Header.TotalValue != 50.00 {
		t.Errorf("Header.TotalValue = %f, want 50.00 (5000 pence)", f.Header.TotalValue)
	}
	if len(f.DirectDebits) != 1 {
		t.Fatalf("expected 1 dd, got %d", len(f.DirectDebits))
	}
	if f.DirectDebits[0].Amount != 19.99 {
		t.Errorf("DD Amount = %f, want 19.99 (1999 pence)", f.DirectDebits[0].Amount)
	}
	if f.Trailer9.TotalValue != 19.99 {
		t.Errorf("Trailer9.TotalValue = %f, want 19.99 (1999 pence)", f.Trailer9.TotalValue)
	}
}

func TestParseMultipleRecords(t *testing.T) {
	content := rec1("1", "654321", "01234567", 8000, 3, "260526") + "\n" +
		rec3("1", "654321", "01234567", 1000, "ORIGINATOR", "1", "REF1", "XX1") + "\n" +
		rec3("1", "654321", "01234567", 2000, "ORIGINATOR", "1", "REF2", "XX2") + "\n" +
		rec4("1", "654321", "01234567", 5000, "ORIGINATOR", "PAYROLL", "001") + "\n" +
		rec5("1", 3) + "\n" +
		rec9("1", 8000, 3, 30) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(f.DirectDebits) != 2 {
		t.Errorf("DirectDebits = %d, want 2", len(f.DirectDebits))
	}
	if len(f.DirectCredits) != 1 {
		t.Errorf("DirectCredits = %d, want 1", len(f.DirectCredits))
	}
	if f.Trailer5.RecordCount != 3 {
		t.Errorf("RecordCount = %d, want 3", f.Trailer5.RecordCount)
	}
}

func TestParse_LinePadding(t *testing.T) {
	content := rec1("1", "654321", "01234567", 10000, 1, "260526")
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if f.Header == nil {
		t.Fatal("expected parsed header")
	}
}

func TestParse_VolumeNoZero(t *testing.T) {
	content := rec1("0", "654321", "01234567", 0, 0, "260526") + "\n" +
		rec5("0", 0) + "\n" +
		rec9("0", 0, 0, 0) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if f.Header.VolumeNo != 0 {
		t.Errorf("VolumeNo = %d, want 0", f.Header.VolumeNo)
	}
	if f.Header.TotalValue != 0 {
		t.Errorf("TotalValue = %f, want 0", f.Header.TotalValue)
	}
}

func TestParse_MultipleAUDDIS(t *testing.T) {
	content := rec1("1", "654321", "01234567", 10000, 1, "260526") + "\n" +
		rec3("1", "654321", "01234567", 10000, "ORIGINATOR", "1", "REF", "XX1") + "\n" +
		rec5("1", 1) + "\n" +
		rec9("1", 10000, 1, 10) + "\n" +
		recA("1", "N", "654321", "01234567", "FIRST MANDATE", 5000) + "\n" +
		recA("1", "C", "654321", "01234567", "SECOND MANDATE", 10000) + "\n"
	f, err := standard18.Parse(content)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(f.AuddisItems) != 2 {
		t.Fatalf("AUDDIS items = %d, want 2", len(f.AuddisItems))
	}
	if f.AuddisItems[0].Instruction != "N" {
		t.Errorf("First instruction = %q, want %q", f.AuddisItems[0].Instruction, "N")
	}
	if f.AuddisItems[1].Instruction != "C" {
		t.Errorf("Second instruction = %q, want %q", f.AuddisItems[1].Instruction, "C")
	}
	if f.AuddisItems[0].Amount != 50.00 {
		t.Errorf("First AUDDIS amount = %f, want 50.00", f.AuddisItems[0].Amount)
	}
	if f.AuddisItems[1].Amount != 100.00 {
		t.Errorf("Second AUDDIS amount = %f, want 100.00", f.AuddisItems[1].Amount)
	}
}
