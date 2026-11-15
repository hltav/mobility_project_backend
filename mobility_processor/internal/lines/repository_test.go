package lines

import (
	"mobility_processor/internal/sptrans"
	"testing"
)

func TestFromSPTransDoesNotMergeCodesOrDirections(t *testing.T) {
	a := FromSPTrans(sptrans.RawLine{CL: 999, LT: "0008", TL: 21, SL: 2, TP: "Principal", TS: "Secondary"})
	if a.Code != "0008-21" || a.SptransID != 999 || a.Origin != "Principal" || a.Destination != "Secondary" {
		t.Fatal(a)
	}
}
