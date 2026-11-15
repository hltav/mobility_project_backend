package parser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PrepareOptions struct {
	MaxRows, ChunkRows, MaxGroupRows int
	MetadataRows                     int
	TempDir                          string
	CollectIssues                    bool
}
type StreamStats struct {
	Counts             map[string]int `json:"rows_by_file"`
	Expected           map[string]int `json:"expected_table_rows"`
	MaxGroupRows       map[string]int `json:"maximum_group_rows"`
	ExtendedTimeFields int            `json:"time_fields_over_24h"`
	MaxServiceSeconds  int            `json:"maximum_service_seconds"`
	TemporaryBytes     int64          `json:"prepared_temporary_bytes"`
	IgnoredFiles       []string       `json:"unmodeled_files"`
}
type ValidationIssue struct {
	File     string `json:"file"`
	SourceID string `json:"source_id"`
	Message  string `json:"message"`
}
type PreparedFeed struct {
	Issues     []ValidationIssue `json:"validation_errors,omitempty"`
	Metadata   *Dataset          `json:"-"`
	Index      *MetadataIndex    `json:"-"`
	ShapeIDs   map[string]bool   `json:"-"`
	Checksum   string            `json:"checksum_sha256"`
	Stats      StreamStats       `json:"statistics"`
	paths      map[string]string
	root       string
	groupLimit int
}

func (p *PreparedFeed) Close() error {
	if p.root == "" {
		return nil
	}
	return os.RemoveAll(p.root)
}
func (p *PreparedFeed) Rows(ctx context.Context, file string, fn func(Raw) error) error {
	path, ok := p.paths[file]
	if !ok {
		return nil
	}
	return walkSorted(ctx, path, fn)
}

// Groups are externally sorted by textual identity and numeric sequence, with a separate group bound.
func (p *PreparedFeed) Groups(ctx context.Context, file string, fn func(*Dataset) error) error {
	var current string
	group := &Dataset{}
	n := 0
	flush := func() error {
		if n == 0 {
			return nil
		}
		return fn(group)
	}
	err := p.Rows(ctx, file, func(r Raw) error {
		id := r["trip_id"]
		if file == "shapes.txt" {
			id = r["shape_id"]
		}
		if n > 0 && id != current {
			if e := flush(); e != nil {
				return e
			}
			group = &Dataset{}
			n = 0
		}
		current = id
		n++
		if n > p.groupLimit {
			return fmt.Errorf("%s group exceeds explicit memory limit", file)
		}
		return group.add(file, r)
	})
	if err != nil {
		return err
	}
	return flush()
}
func Prepare(ctx context.Context, dir string, opt PrepareOptions) (plan *PreparedFeed, err error) {
	if opt.MaxRows <= 0 || opt.ChunkRows < 1 || opt.ChunkRows > 100000 {
		return nil, fmt.Errorf("positive row budget and bounded chunk size required")
	}
	if opt.MaxGroupRows == 0 {
		opt.MaxGroupRows = 20000
	}
	if opt.MetadataRows == 0 {
		opt.MetadataRows = 100000
	}
	if opt.MetadataRows < 1 || opt.MetadataRows > 100000 {
		return nil, fmt.Errorf("metadata row bound must be 1..100000")
	}
	if opt.MaxGroupRows < 1 || opt.MaxGroupRows > 100000 {
		return nil, fmt.Errorf("positive group limit required")
	}
	root, e := os.MkdirTemp(opt.TempDir, "mobility-gtfs-prepare-")
	if e != nil {
		return nil, e
	}
	p := &PreparedFeed{Metadata: &Dataset{}, ShapeIDs: map[string]bool{}, paths: map[string]string{}, root: root, groupLimit: opt.MaxGroupRows, Stats: StreamStats{Counts: map[string]int{}, Expected: map[string]int{}, MaxGroupRows: map[string]int{}}}
	defer func() {
		if err != nil {
			p.Close()
		}
	}()
	p.Checksum, err = Checksum(ctx, dir)
	if err != nil {
		return nil, err
	}
	issue := func(file, id string, e error) error {
		if e == nil {
			return nil
		}
		if !opt.CollectIssues {
			return fmt.Errorf("%s source_id=%s: %w", file, id, e)
		}
		p.Issues = append(p.Issues, ValidationIssue{file, id, e.Error()})
		return nil
	}
	total := 0
	count := func(file string) func(Raw) error {
		return func(r Raw) error {
			total++
			if total > opt.MaxRows {
				return fmt.Errorf("total row budget exceeded")
			}
			p.Stats.Counts[file]++
			return nil
		}
	}
	small := []string{"agency.txt", "routes.txt", "stops.txt", "calendar.txt", "calendar_dates.txt", "trips.txt"}
	metadataRows := 0
	for _, file := range small {
		path := filepath.Join(dir, file)
		_, e := os.Stat(path)
		if os.IsNotExist(e) && (file == "calendar.txt" || file == "calendar_dates.txt") {
			continue
		}
		if e != nil {
			return nil, e
		}
		err = read(path, headers[file], 0, func(r Raw) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			if e := count(file)(r); e != nil {
				return e
			}
			metadataRows++
			if metadataRows > opt.MetadataRows {
				return fmt.Errorf("metadata row memory bound exceeded")
			}
			return p.Metadata.add(file, r)
		})
		if err != nil {
			return nil, err
		}
	}
	p.Index, err = p.Metadata.ValidateMetadata()
	if err != nil {
		return nil, err
	}
	for _, file := range []string{"shapes.txt", "stop_times.txt", "frequencies.txt"} {
		path := filepath.Join(dir, file)
		_, e := os.Stat(path)
		if os.IsNotExist(e) && file != "stop_times.txt" {
			continue
		}
		if e != nil {
			return nil, e
		}
		sorted, e := externalSort(ctx, path, file, root, opt.ChunkRows, count(file))
		if e != nil {
			return nil, e
		}
		p.paths[file] = sorted
	}
	err = p.Groups(ctx, "shapes.txt", func(d *Dataset) error {
		if e := issue("shapes.txt", d.Shapes[0].ShapeID, ValidateShapeGroup(d.Shapes)); e != nil {
			return e
		}
		p.ShapeIDs[d.Shapes[0].ShapeID] = true
		p.Stats.MaxGroupRows["shapes.txt"] = max(p.Stats.MaxGroupRows["shapes.txt"], len(d.Shapes))
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, t := range p.Metadata.Trips {
		if t.ShapeID != "" && !p.ShapeIDs[t.ShapeID] {
			return nil, fmt.Errorf("trip references unknown shape")
		}
	}
	tripsSeen := map[string]bool{}
	patterns := map[string]bool{}
	patternStops := 0
	err = p.Groups(ctx, "stop_times.txt", func(d *Dataset) error {
		if e := issue("stop_times.txt", d.StopTimes[0].GTFSTripID, p.Index.ValidateStopTimeGroup(d.StopTimes)); e != nil {
			return e
		}
		id := d.StopTimes[0].GTFSTripID
		tripsSeen[id] = true
		fingerprint := PatternFingerprint(p.Index.Trips[id], d.StopTimes)
		if !patterns[fingerprint] {
			patterns[fingerprint] = true
			patternStops += len(d.StopTimes)
		}
		p.Stats.MaxGroupRows["stop_times.txt"] = max(p.Stats.MaxGroupRows["stop_times.txt"], len(d.StopTimes))
		for _, s := range d.StopTimes {
			for _, v := range []*int{s.ArrivalSeconds, s.DepartureSeconds} {
				if v != nil {
					p.Stats.MaxServiceSeconds = max(p.Stats.MaxServiceSeconds, *v)
					if *v >= 86400 {
						p.Stats.ExtendedTimeFields++
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for id := range p.Index.Trips {
		if !tripsSeen[id] {
			return nil, fmt.Errorf("trip has no stop_times")
		}
	}
	err = p.Groups(ctx, "frequencies.txt", func(d *Dataset) error {
		if e := issue("frequencies.txt", d.Frequencies[0].GTFSTripID, p.Index.ValidateFrequencyGroup(d.Frequencies)); e != nil {
			return e
		}
		p.Stats.MaxGroupRows["frequencies.txt"] = max(p.Stats.MaxGroupRows["frequencies.txt"], len(d.Frequencies))
		return nil
	})
	if err != nil {
		return nil, err
	}
	current, e := Checksum(ctx, dir)
	if e != nil {
		return nil, e
	}
	if current != p.Checksum {
		return nil, fmt.Errorf("source changed during preparation")
	}
	services := map[string]bool{}
	for _, c := range p.Metadata.Calendars {
		services[c.ServiceID] = true
	}
	for _, e := range p.Metadata.Exceptions {
		services[e.ServiceID] = true
	}
	p.Stats.Expected = map[string]int{"agencies": len(p.Metadata.Agencies), "routes": len(p.Metadata.Routes), "stops": len(p.Metadata.Stops), "services": len(services), "calendars": len(p.Metadata.Calendars), "service_exceptions": len(p.Metadata.Exceptions), "shapes": len(p.ShapeIDs), "shape_points": p.Stats.Counts["shapes.txt"], "trips": len(p.Metadata.Trips), "stop_times": p.Stats.Counts["stop_times.txt"], "frequencies": p.Stats.Counts["frequencies.txt"], "journey_patterns": len(patterns), "journey_pattern_stops": patternStops}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	for _, f := range entries {
		if strings.HasSuffix(f.Name(), ".txt") {
			if _, ok := headers[f.Name()]; !ok {
				p.Stats.IgnoredFiles = append(p.Stats.IgnoredFiles, f.Name())
			}
		}
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			i, e := d.Info()
			if e != nil {
				return e
			}
			p.Stats.TemporaryBytes += i.Size()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

func DecodeRows(file string, rows []Raw) (*Dataset, error) {
	d := &Dataset{}
	for _, r := range rows {
		if err := d.add(file, r); err != nil {
			return nil, err
		}
	}
	return d, nil
}
