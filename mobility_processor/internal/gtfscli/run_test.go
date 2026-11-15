package gtfscli

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDryRunNeedsNoDatabaseAndLegacyFreshIsRejected(t *testing.T) {
	t.Setenv("DB_CONNECTION", "mysql")
	var out bytes.Buffer
	args := []string{"--data-dir", "../gtfs/testdata/schedule", "--provider", "fixture", "--source", "fixture"}
	if e := Run(args, &out); e != nil {
		t.Fatal(e)
	}
	var result map[string]any
	if e := json.Unmarshal(out.Bytes(), &result); e != nil || result["dry_run"] != true {
		t.Fatal(out.String(), e)
	}
	if e := Run(append(args, "--fresh"), &out); e == nil {
		t.Fatal("destructive legacy option accepted")
	}
	if e := Run(nil, &out); e == nil {
		t.Fatal("implicit real data directory accepted")
	}
}
