package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	entries, e := os.ReadDir("../testdata/schedule")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range entries {
		b, e := os.ReadFile(filepath.Join("../testdata/schedule", f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, f.Name()), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return dir
}
func TestServiceSeconds(t *testing.T) {
	for _, s := range []string{"00:00:00", "9:01:02", "25:00:00", "100:59:59"} {
		v, e := ServiceSeconds(s)
		if e != nil || v == nil {
			t.Fatalf("%s: %v", s, e)
		}
	}
	v, e := ServiceSeconds("25:00:00")
	if e != nil || *v != 90000 {
		t.Fatal(v, e)
	}
	v, e = ServiceSeconds("")
	if e != nil || v != nil {
		t.Fatal(v, e)
	}
	for _, s := range []string{"-1:00:00", "25:60:00", "25:00:60", "1:2:03", "abc", "NaN", "999999999999:00:00"} {
		if _, e := ServiceSeconds(s); e == nil {
			t.Fatalf("accepted invalid time %q", s)
		}
	}
}
func TestLoadPreservesIdentityNullabilityAndCalendars(t *testing.T) {
	d, e := Load(fixture(t), 100)
	if e != nil {
		t.Fatal(e)
	}
	if d.Stops[1].GTFSStopID != "000123" || d.Trips[3].DirectionID != nil || d.Shapes[0].DistanceTraveled != nil || d.Shapes[2].DistanceTraveled == nil || *d.Shapes[2].DistanceTraveled != 0 {
		t.Fatal("identity/nullability lost")
	}
	if *d.StopTimes[0].ArrivalSeconds != 90000 || d.Frequencies[1].ExactTimes != 1 || d.Frequencies[2].RawExactTimes != "" || len(d.Calendars) != 1 || len(d.Exceptions) != 2 {
		t.Fatal("Schedule semantics lost")
	}
}
func TestRejectMalformedOrInconsistentFixtures(t *testing.T) {
	cases := []struct{ name, file, from, to string }{
		{"bad numeric", "stops.txt", "-23.55,-46.63,0", "invalid,-46.63,0"},
		{"NaN", "shapes.txt", "-23.551,-46.631,30", "NaN,-46.631,30"},
		{"invalid direction", "trips.txt", "Main,0,SH-0", "Main,2,SH-0"},
		{"unknown route", "trips.txt", "R000,WK", "missing,WK"},
		{"unknown service", "trips.txt", "R000,WK", "R000,missing"},
		{"unknown stop", "stop_times.txt", "25:00:10,000123", "25:00:10,unknown"},
		{"unknown shape", "trips.txt", "Main,0,SH-0", "Main,0,unknown"},
		{"bad headway", "frequencies.txt", "600,0", "0,0"},
		{"bad calendar", "calendar.txt", "20260101,20261231", "20261231,20260101"},
		{"overlap", "frequencies.txt", "26:00:00,27:00:00", "25:30:00,27:00:00"},
		{"invalid timestamp", "stop_times.txt", "25:00:00", "25:99:00"},
		{"nonmonotonic", "stop_times.txt", "25:01:00,25:01:10", "24:01:00,24:01:10"},
		{"bad timezone", "agency.txt", "America/Sao_Paulo", "Unknown/Timezone"},
		{"parent cycle", "stops.txt", ",4,000123,", ",4,BA,"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := fixture(t)
			p := filepath.Join(dir, c.file)
			b, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			updated := strings.Replace(string(b), c.from, c.to, 1)
			if updated == string(b) {
				t.Fatal("fixture mutation did not apply")
			}
			if e = os.WriteFile(p, []byte(updated), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e = Load(dir, 100); e == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}
func TestMissingHeadersDuplicateRowsAndBudget(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		dir := fixture(t)
		os.Remove(filepath.Join(dir, "agency.txt"))
		if _, e := Load(dir, 100); e == nil {
			t.Fatal("missing agency accepted")
		}
	})
	t.Run("missing header", func(t *testing.T) {
		dir := fixture(t)
		p := filepath.Join(dir, "routes.txt")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), "route_id", "wrong", 1)), 0600)
		if _, e := Load(dir, 100); e == nil {
			t.Fatal("missing header accepted")
		}
	})
	t.Run("duplicate header", func(t *testing.T) {
		dir := fixture(t)
		os.WriteFile(filepath.Join(dir, "routes.txt"), []byte("route_id,route_id,route_type\na,a,3\n"), 0600)
		if _, e := Load(dir, 100); e == nil {
			t.Fatal("duplicate header accepted")
		}
	})
	t.Run("duplicate natural key", func(t *testing.T) {
		dir := fixture(t)
		p := filepath.Join(dir, "frequencies.txt")
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
		f.WriteString("T-A,25:00:00,26:00:00,600,0\n")
		f.Close()
		if _, e := Load(dir, 100); e == nil {
			t.Fatal("duplicate frequency accepted")
		}
	})
	t.Run("budget", func(t *testing.T) {
		if _, e := Load(fixture(t), 2); e == nil {
			t.Fatal("row budget ignored")
		}
	})
}
func TestCalendarDatesOnlyAndOptionalFiles(t *testing.T) {
	dir := fixture(t)
	os.Remove(filepath.Join(dir, "calendar.txt"))
	p := filepath.Join(dir, "calendar_dates.txt")
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("WK,20261025,1\n")
	f.Close()
	os.Remove(filepath.Join(dir, "frequencies.txt"))
	if _, e := Load(dir, 100); e != nil {
		t.Fatal(e)
	}
}
func TestChecksumChangesWithArtifactAndContextCancellation(t *testing.T) {
	dir := fixture(t)
	a, e := Checksum(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "fare_attributes.txt"), []byte("fare_id,price\nF,1\n"), 0600)
	b, e := Checksum(context.Background(), dir)
	if e != nil || a == b {
		t.Fatal("artifact not fingerprinted", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = LoadContext(ctx, dir, 100); e == nil {
		t.Fatal("cancellation ignored")
	}
}
