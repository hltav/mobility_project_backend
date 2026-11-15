package parser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type Agency struct {
	ID, Name, URL, Timezone, Language string
	Raw                               Raw
}
type Route struct {
	GTFSRouteID, AgencyID, ShortName, LongName, Color, TextColor string
	RouteType                                                    int
	Raw                                                          Raw
}
type Stop struct {
	GTFSStopID, Code, Name, Description, ParentID, Timezone, ZoneID string
	Lat, Lng                                                        *float64
	LocationType                                                    int
	Wheelchair                                                      *int
	Raw                                                             Raw
}
type Trip struct {
	GTFSTripID, GTFSRouteID, ServiceID, Headsign, ShapeID, ShortName, BlockID string
	DirectionID, Wheelchair, Bikes                                            *int
	Raw                                                                       Raw
}
type StopTime struct {
	GTFSTripID, GTFSStopID, ArrivalTime, DepartureTime, Headsign string
	StopSequence                                                 int
	ArrivalSeconds, DepartureSeconds, Pickup, DropOff, Timepoint *int
	Distance                                                     *float64
	Raw                                                          Raw
}
type ShapePoint struct {
	ShapeID          string
	Lat, Lng         float64
	Sequence         int
	DistanceTraveled *float64
	Raw              Raw
}
type Frequency struct {
	GTFSTripID, StartTime, EndTime, RawExactTimes     string
	StartSeconds, EndSeconds, HeadwaySecs, ExactTimes int
	Raw                                               Raw
}
type Calendar struct {
	ServiceID, StartDate, EndDate string
	Days                          [7]bool
	Raw                           Raw
}
type Exception struct {
	ServiceID, Date string
	Type            int
	Raw             Raw
}
type Dataset struct {
	Agencies    []Agency
	Routes      []Route
	Stops       []Stop
	Trips       []Trip
	StopTimes   []StopTime
	Shapes      []ShapePoint
	Frequencies []Frequency
	Calendars   []Calendar
	Exceptions  []Exception
	Checksum    string
	Counts      map[string]int
}

func date(s string) (string, error) {
	t, e := time.Parse("20060102", s)
	if e != nil {
		return "", fmt.Errorf("invalid service date")
	}
	return t.Format("2006-01-02"), nil
}

// Load is intentionally bounded for the Stage 2 profile. It never reads .env or performs network I/O.
func Load(dir string, maxRows int) (*Dataset, error) {
	return LoadContext(context.Background(), dir, maxRows)
}
func LoadContext(ctx context.Context, dir string, maxRows int) (*Dataset, error) {
	if maxRows <= 0 {
		return nil, fmt.Errorf("positive row budget required")
	}
	d := &Dataset{Counts: map[string]int{}}
	files := []string{"agency.txt", "routes.txt", "stops.txt", "calendar.txt", "calendar_dates.txt", "shapes.txt", "trips.txt", "stop_times.txt", "frequencies.txt"}
	budget := maxRows
	initial, err := Checksum(ctx, dir)
	if err != nil {
		return nil, err
	}
	d.Checksum = initial

	for _, name := range files {
		path := filepath.Join(dir, name)
		_, e := os.Stat(path)
		required := name == "agency.txt" || name == "routes.txt" || name == "stops.txt" || name == "trips.txt" || name == "stop_times.txt"
		if os.IsNotExist(e) && !required {
			continue
		}
		if e != nil {
			return nil, e
		}
		err := read(path, headers[name], maxRows, func(r Raw) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			budget--
			if budget < 0 {
				return fmt.Errorf("total row budget exceeded")
			}
			d.Counts[name]++
			return d.add(name, r)
		})
		if err != nil {
			return nil, err
		}
	}
	current, err := Checksum(ctx, dir)
	if err != nil {
		return nil, err
	}
	if initial != current {
		return nil, fmt.Errorf("GTFS files changed during parsing")
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

var headers = map[string][]string{
	"agency.txt": {"agency_name", "agency_url", "agency_timezone"}, "routes.txt": {"route_id", "route_type"}, "stops.txt": {"stop_id"}, "trips.txt": {"route_id", "service_id", "trip_id"},
	"stop_times.txt": {"trip_id", "stop_id", "stop_sequence"}, "calendar.txt": {"service_id", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday", "start_date", "end_date"},
	"calendar_dates.txt": {"service_id", "date", "exception_type"}, "shapes.txt": {"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"}, "frequencies.txt": {"trip_id", "start_time", "end_time", "headway_secs"},
}

func (d *Dataset) add(file string, r Raw) error {
	// Parse every numeric value explicitly: invalid or absent never silently becomes zero.
	switch file {
	case "agency.txt":
		name, e := r.Required("agency_name")
		if e != nil {
			return e
		}
		u, e := r.Required("agency_url")
		if e != nil {
			return e
		}
		parsedURL, urlErr := url.ParseRequestURI(u)
		if urlErr != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return fmt.Errorf("invalid agency URL")
		}
		tz, e := r.Required("agency_timezone")
		if e != nil {
			return e
		}
		if _, e = time.LoadLocation(tz); e != nil {
			return fmt.Errorf("unknown agency timezone")
		}
		d.Agencies = append(d.Agencies, Agency{r["agency_id"], name, u, tz, r["agency_lang"], r})
	case "routes.txt":
		id, e := r.Required("route_id")
		if e != nil {
			return e
		}
		kind, e := r.Integer("route_type", false)
		if e != nil || *kind < 0 {
			if e != nil {
				return e
			}
			return fmt.Errorf("negative route_type")
		}
		d.Routes = append(d.Routes, Route{id, r["agency_id"], r["route_short_name"], r["route_long_name"], r["route_color"], r["route_text_color"], *kind, r})
	case "stops.txt":
		id, e := r.Required("stop_id")
		if e != nil {
			return e
		}
		lat, e := r.Number("stop_lat", true)
		if e != nil {
			return e
		}
		lon, e := r.Number("stop_lon", true)
		if e != nil {
			return e
		}
		loc, e := r.Enum("location_type", true, 0, 1, 2, 3, 4)
		if e != nil {
			return e
		}
		lt := 0
		if loc != nil {
			lt = *loc
		}
		w, e := r.Enum("wheelchair_boarding", true, 0, 1, 2)
		if e != nil {
			return e
		}
		d.Stops = append(d.Stops, Stop{id, r["stop_code"], r["stop_name"], r["stop_desc"], r["parent_station"], r["stop_timezone"], r["zone_id"], lat, lon, lt, w, r})
	case "trips.txt":
		id, e := r.Required("trip_id")
		if e != nil {
			return e
		}
		route, e := r.Required("route_id")
		if e != nil {
			return e
		}
		svc, e := r.Required("service_id")
		if e != nil {
			return e
		}
		direction, e := r.Enum("direction_id", true, 0, 1)
		if e != nil {
			return e
		}
		w, e := r.Enum("wheelchair_accessible", true, 0, 1, 2)
		if e != nil {
			return e
		}
		b, e := r.Enum("bikes_allowed", true, 0, 1, 2)
		if e != nil {
			return e
		}
		d.Trips = append(d.Trips, Trip{id, route, svc, r["trip_headsign"], r["shape_id"], r["trip_short_name"], r["block_id"], direction, w, b, r})
	case "stop_times.txt":
		trip, e := r.Required("trip_id")
		if e != nil {
			return e
		}
		stop, e := r.Required("stop_id")
		if e != nil {
			return e
		}
		seq, e := r.Integer("stop_sequence", false)
		if e != nil {
			return e
		}
		a, e := ServiceSeconds(r["arrival_time"])
		if e != nil {
			return e
		}
		dep, e := ServiceSeconds(r["departure_time"])
		if e != nil {
			return e
		}
		p, e := r.Enum("pickup_type", true, 0, 1, 2, 3)
		if e != nil {
			return e
		}
		q, e := r.Enum("drop_off_type", true, 0, 1, 2, 3)
		if e != nil {
			return e
		}
		tp, e := r.Enum("timepoint", true, 0, 1)
		if e != nil {
			return e
		}
		dist, e := r.Number("shape_dist_traveled", true)
		if e != nil {
			return e
		}
		d.StopTimes = append(d.StopTimes, StopTime{trip, stop, r["arrival_time"], r["departure_time"], r["stop_headsign"], *seq, a, dep, p, q, tp, dist, r})
	case "shapes.txt":
		id, e := r.Required("shape_id")
		if e != nil {
			return e
		}
		lat, e := r.Number("shape_pt_lat", false)
		if e != nil {
			return e
		}
		lon, e := r.Number("shape_pt_lon", false)
		if e != nil {
			return e
		}
		seq, e := r.Integer("shape_pt_sequence", false)
		if e != nil {
			return e
		}
		dist, e := r.Number("shape_dist_traveled", true)
		if e != nil {
			return e
		}
		d.Shapes = append(d.Shapes, ShapePoint{id, *lat, *lon, *seq, dist, r})
	case "frequencies.txt":
		trip, e := r.Required("trip_id")
		if e != nil {
			return e
		}
		start, e := ServiceSeconds(r["start_time"])
		if e != nil || start == nil {
			return fmt.Errorf("invalid frequency start_time")
		}
		end, e := ServiceSeconds(r["end_time"])
		if e != nil || end == nil {
			return fmt.Errorf("invalid frequency end_time")
		}
		head, e := r.Integer("headway_secs", false)
		if e != nil {
			return e
		}
		exact, e := r.Enum("exact_times", true, 0, 1)
		if e != nil {
			return e
		}
		et := 0
		if exact != nil {
			et = *exact
		}
		d.Frequencies = append(d.Frequencies, Frequency{trip, r["start_time"], r["end_time"], r["exact_times"], *start, *end, *head, et, r})
	case "calendar.txt":
		id, e := r.Required("service_id")
		if e != nil {
			return e
		}
		a, e := date(r["start_date"])
		if e != nil {
			return e
		}
		b, e := date(r["end_date"])
		if e != nil {
			return e
		}
		c := Calendar{ServiceID: id, StartDate: a, EndDate: b, Raw: r}
		for i, key := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
			v, e := r.Enum(key, false, 0, 1)
			if e != nil {
				return e
			}
			c.Days[i] = *v == 1
		}
		d.Calendars = append(d.Calendars, c)
	case "calendar_dates.txt":
		id, e := r.Required("service_id")
		if e != nil {
			return e
		}
		day, e := date(r["date"])
		if e != nil {
			return e
		}
		kind, e := r.Enum("exception_type", false, 1, 2)
		if e != nil {
			return e
		}
		d.Exceptions = append(d.Exceptions, Exception{id, day, *kind, r})
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func Checksum(ctx context.Context, dir string) (string, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	h := sha256.New()
	for _, path := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return "", err
		}
		name := filepath.Base(path)
		fmt.Fprintf(h, "%d:%s:%d:", len(name), name, info.Size())
		_, err = io.Copy(h, contextReader{ctx, f})
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
