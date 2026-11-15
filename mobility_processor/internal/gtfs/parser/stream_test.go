package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStreamingPreparationMatchesBoundedFixture(t *testing.T) {
	dir := fixture(t)
	bounded, e := Load(dir, 100)
	if e != nil {
		t.Fatal(e)
	}
	p, e := Prepare(context.Background(), dir, PrepareOptions{MaxRows: 100, ChunkRows: 2})
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if !reflect.DeepEqual(p.Stats.Counts, bounded.Counts) || p.Stats.Expected["journey_patterns"] != 3 || p.Stats.Expected["shape_points"] != 4 || p.Stats.MaxServiceSeconds != 93730 {
		t.Fatal(p.Stats)
	}
	if len(p.Metadata.Shapes) != 0 || len(p.Metadata.StopTimes) != 0 || len(p.Metadata.Frequencies) != 0 {
		t.Fatal("large entities retained in metadata")
	}
	path := p.root
	if e = p.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("private spool not removed")
	}
}
func TestStreamingSortSupportsInterleavedEntitiesAndPreservesTextIDs(t *testing.T) {
	dir := fixture(t)
	path := filepath.Join(dir, "stop_times.txt")
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i, j := 1, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	if e = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e := Prepare(context.Background(), dir, PrepareOptions{MaxRows: 100, ChunkRows: 1})
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	seen := false
	if e = p.Groups(context.Background(), "stop_times.txt", func(d *Dataset) error {
		for _, s := range d.StopTimes {
			if s.GTFSStopID == "000123" {
				seen = true
			}
		}
		return nil
	}); e != nil || !seen {
		t.Fatal(e)
	}
}
func TestRealSampleAndSingleStopBlocker(t *testing.T) {
	p, e := Prepare(context.Background(), "../testdata/sptrans_real_sample", PrepareOptions{MaxRows: 20000, ChunkRows: 128})
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if p.Stats.ExtendedTimeFields == 0 || p.Stats.MaxGroupRows["shapes.txt"] != 1824 {
		t.Fatal("real coverage absent", p.Stats)
	}
	if _, e = Prepare(context.Background(), "../testdata/sptrans_invalid_single_stop", PrepareOptions{MaxRows: 10000, ChunkRows: 128}); e == nil {
		t.Fatal("real single-stop defect ignored")
	}
	bad, e := Prepare(context.Background(), "../testdata/sptrans_invalid_single_stop", PrepareOptions{MaxRows: 10000, ChunkRows: 128, CollectIssues: true})
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Close()
	if len(bad.Issues) != 1 || bad.Issues[0].SourceID != "626A-10-1" {
		t.Fatal(bad.Issues)
	}
}
func TestStreamingGroupBoundAndCancellation(t *testing.T) {
	if _, e := Prepare(context.Background(), fixture(t), PrepareOptions{MaxRows: 100, ChunkRows: 2, MaxGroupRows: 2}); e == nil {
		t.Fatal("group memory bound ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Prepare(ctx, fixture(t), PrepareOptions{MaxRows: 100, ChunkRows: 2}); e == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestStage3MillionPointStream(t *testing.T) {
	if os.Getenv("GTFS_STAGE3_VOLUME_TESTS") != "1" {
		t.Skip("explicit read-only volume-test opt-in")
	}
	dir := fixture(t)
	path := filepath.Join(dir, "shapes.txt")
	f, e := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	for group := 0; group < 1000; group++ {
		for seq := 0; seq < 1001; seq++ {
			if _, e = fmt.Fprintf(f, "V-%04d,-23.55,-46.%06d,%d,\n", group, 630000+seq, seq); e != nil {
				f.Close()
				t.Fatal(e)
			}
		}
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	p, e := Prepare(context.Background(), dir, PrepareOptions{MaxRows: 1100000, ChunkRows: 4096})
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if p.Stats.Counts["shapes.txt"] != 1001004 || p.Stats.MaxGroupRows["shapes.txt"] != 1001 || len(p.Metadata.Shapes) != 0 {
		t.Fatal(p.Stats)
	}
	t.Logf("read-only volume: rows=%d temporary_bytes=%d maximum_group=%d", p.Stats.Counts["shapes.txt"], p.Stats.TemporaryBytes, p.Stats.MaxGroupRows["shapes.txt"])
}
