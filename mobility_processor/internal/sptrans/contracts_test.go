package sptrans

import (
	"encoding/json"
	"testing"
)

func TestOfficialLineFieldsRetainTypesAndRejectMissingDirection(t *testing.T) {
	var line RawLine
	if e := json.Unmarshal([]byte(`{"cl":998,"lc":false,"lt":"0008","tl":21,"sl":2,"tp":"Primary","ts":"Secondary"}`), &line); e != nil || line.CL != 998 || line.TL != 21 || line.SL != 2 || line.LT != "0008" || line.LC {
		t.Fatal(line, e)
	}
	for _, input := range []string{`{"cl":998,"lc":false,"lt":"0008","tl":21}`, `{"cl":998,"lc":false,"lt":"0008","tl":21,"sl":0}`, `{"cl":998,"lt":"0008","tl":21,"sl":1}`} {
		if e := json.Unmarshal([]byte(input), &line); e == nil {
			t.Fatal("missing/invalid official field accepted")
		}
	}
}
func TestOfficialStopCoordinatesDistinguishZeroAndAbsence(t *testing.T) {
	var stop Stop
	if e := json.Unmarshal([]byte(`{"cp":123,"np":"Fixture","py":0,"px":0}`), &stop); e != nil || stop.CP != 123 || stop.Lat != 0 || stop.Lng != 0 {
		t.Fatal(stop, e)
	}
	for _, input := range []string{`{"cp":123,"np":"Fixture"}`, `{"cp":123,"np":"Fixture","py":null,"px":0}`, `{"cp":123,"np":"Fixture","py":91,"px":0}`} {
		if e := json.Unmarshal([]byte(input), &stop); e == nil {
			t.Fatal("missing/invalid coordinates accepted")
		}
	}
}
