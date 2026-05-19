package standard18

import (
	"fmt"
	"strconv"
	"strings"
)

type Record1 struct {
	Type          string
	VolumeNo      int
	DestSortCode  string
	DestAccount   string
	TotalValue    float64
	TotalVolume   int
	Date          string
}

type Record3 struct {
	Type             string
	VolumeHeaderNo   int
	DestSortCode     string
	DestAccount      string
	Amount           float64
	OriginatorSortAcc string
	TransType        string
	Reference        string
	SUCode           string
}

type Record4 struct {
	Type           string
	VolumeHeaderNo int
	DestSortCode   string
	DestAccount    string
	Amount         float64
	OriginatorName string
	PayrollRef     string
	SUCode         string
}

type Record5 struct {
	Type        string
	VolumeNo    int
	RecordCount int
}

type Record9 struct {
	Type        string
	VolumeNo    int
	TotalValue  float64
	TotalVolume int
	HashTotal   int
}

type AUDDIS struct {
	Type           string
	VolumeHeaderNo int
	Instruction    string
	SortCode       string
	Account        string
	Ref            string
	Amount         float64
}

type File struct {
	Header        *Record1
	DirectDebits  []Record3
	DirectCredits []Record4
	Trailer5      *Record5
	Trailer9      *Record9
	AuddisItems   []AUDDIS
}

func Parse(content string) (*File, error) {
	f := &File{}
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if len(line) < 80 && len(line) > 0 {
			padded := line + strings.Repeat(" ", 80-len(line))
			line = padded
		}
		if len(line) == 0 {
			continue
		}
		if len(line) != 80 {
			return nil, fmt.Errorf("line %d: invalid length %d (expected 80)", i+1, len(line))
		}

		recType := string(line[0])
		switch recType {
		case "1":
			volNo, _ := strconv.Atoi(strings.TrimSpace(line[1:8]))
			vol, _ := strconv.Atoi(strings.TrimSpace(line[66:73]))
			val, _ := strconv.ParseFloat(strings.TrimSpace(line[55:66]), 64)
			val = val / 100.0
			f.Header = &Record1{
				Type: recType, VolumeNo: volNo,
				DestSortCode:  strings.TrimSpace(line[8:17]),
				DestAccount:   strings.TrimSpace(line[17:26]),
				TotalValue:    val, TotalVolume: vol,
				Date: strings.TrimSpace(line[73:79]),
			}
		case "3":
			volNo, _ := strconv.Atoi(strings.TrimSpace(line[1:8]))
			amt, _ := strconv.ParseFloat(strings.TrimSpace(line[26:37]), 64)
			amt = amt / 100.0
			f.DirectDebits = append(f.DirectDebits, Record3{
				Type: recType, VolumeHeaderNo: volNo,
				DestSortCode:     strings.TrimSpace(line[8:17]),
				DestAccount:      strings.TrimSpace(line[17:26]),
				Amount:           amt,
				OriginatorSortAcc: strings.TrimSpace(line[37:52]),
				TransType:        string(line[52]),
				Reference:        strings.TrimSpace(line[53:67]),
				SUCode:           strings.TrimSpace(line[67:79]),
			})
		case "4":
			volNo, _ := strconv.Atoi(strings.TrimSpace(line[1:8]))
			amt, _ := strconv.ParseFloat(strings.TrimSpace(line[26:37]), 64)
			amt = amt / 100.0
			f.DirectCredits = append(f.DirectCredits, Record4{
				Type: recType, VolumeHeaderNo: volNo,
				DestSortCode:   strings.TrimSpace(line[8:17]),
				DestAccount:    strings.TrimSpace(line[17:26]),
				Amount:         amt,
				OriginatorName: strings.TrimSpace(line[37:52]),
				PayrollRef:     strings.TrimSpace(line[52:66]),
				SUCode:         strings.TrimSpace(line[66:79]),
			})
		case "5":
			volNo, _ := strconv.Atoi(strings.TrimSpace(line[1:8]))
			recCount, _ := strconv.Atoi(strings.TrimSpace(line[48:56]))
			f.Trailer5 = &Record5{Type: recType, VolumeNo: volNo, RecordCount: recCount}
		case "9":
			volNo, _ := strconv.Atoi(strings.TrimSpace(line[1:8]))
			val, _ := strconv.ParseFloat(strings.TrimSpace(line[20:31]), 64)
			val = val / 100.0
			vol, _ := strconv.Atoi(strings.TrimSpace(line[31:40]))
			hash, _ := strconv.Atoi(strings.TrimSpace(line[40:54]))
			f.Trailer9 = &Record9{Type: recType, VolumeNo: volNo, TotalValue: val, TotalVolume: vol, HashTotal: hash}
		case "A":
			volNo, _ := strconv.Atoi(strings.TrimSpace(line[1:8]))
			amt, _ := strconv.ParseFloat(strings.TrimSpace(line[53:65]), 64)
			amt = amt / 100.0
			f.AuddisItems = append(f.AuddisItems, AUDDIS{
				Type: recType, VolumeHeaderNo: volNo,
				Instruction: strings.TrimSpace(line[8:9]),
				SortCode:    strings.TrimSpace(line[9:18]),
				Account:     strings.TrimSpace(line[18:27]),
				Ref:         strings.TrimSpace(line[27:45]),
				Amount:      amt,
			})
		}
	}
	return f, nil
}

func (f *File) Validate() []string {
	var errs []string
	if f.Header == nil {
		errs = append(errs, "Missing Record 1 (Volume Header)")
	}
	if f.Trailer5 == nil {
		errs = append(errs, "Missing Record 5 (Trailer Label)")
	}
	if f.Trailer9 == nil {
		errs = append(errs, "Missing Record 9 (User Trailer)")
	}
	if f.Trailer9 != nil && f.Header != nil {
		totalItems := len(f.DirectDebits) + len(f.DirectCredits)
		if f.Trailer9.TotalVolume != totalItems {
			errs = append(errs, fmt.Sprintf("Volume mismatch: header=%d, trailer9=%d, actual=%d", f.Header.TotalVolume, f.Trailer9.TotalVolume, totalItems))
		}
	}
	return errs
}
