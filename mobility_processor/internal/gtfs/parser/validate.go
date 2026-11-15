package parser

import (
	"fmt"
	"math"
	"mobility_processor/internal/database"
	"regexp"
	"sort"
	"time"
)

var hexColor = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

func unique(seen map[string]bool, key, entity string) error {
	if seen[key] {
		return fmt.Errorf("duplicate %s natural key", entity)
	}
	seen[key] = true
	return nil
}

type MetadataIndex struct {
	Stops map[string]Stop
	Trips map[string]Trip
}

func (d *Dataset) ValidateMetadata() (*MetadataIndex, error) {
	if len(d.Agencies) == 0 || len(d.Routes) == 0 || len(d.Stops) == 0 || len(d.Trips) == 0 {
		return nil, fmt.Errorf("required Schedule metadata is empty")
	}
	agencies := map[string]bool{}
	for _, a := range d.Agencies {
		if err := unique(agencies, a.ID, "agency"); err != nil {
			return nil, err
		}
		if len(d.Agencies) > 1 && a.ID == "" {
			return nil, fmt.Errorf("multiple agencies require agency_id")
		}
	}
	routes := map[string]bool{}
	for _, r := range d.Routes {
		if err := unique(routes, r.GTFSRouteID, "route"); err != nil {
			return nil, err
		}
		if r.ShortName == "" && r.LongName == "" {
			return nil, fmt.Errorf("route needs short_name or long_name")
		}
		if (r.Color != "" && !hexColor.MatchString(r.Color)) || (r.TextColor != "" && !hexColor.MatchString(r.TextColor)) {
			return nil, fmt.Errorf("invalid route color")
		}
		if r.AgencyID == "" && len(d.Agencies) == 1 {
			continue
		}
		if !agencies[r.AgencyID] {
			return nil, fmt.Errorf("unknown route agency")
		}
	}
	stops := map[string]Stop{}
	for _, s := range d.Stops {
		if _, ok := stops[s.GTFSStopID]; ok {
			return nil, fmt.Errorf("duplicate stop_id")
		}
		stops[s.GTFSStopID] = s
		if s.Timezone != "" {
			if _, e := time.LoadLocation(s.Timezone); e != nil {
				return nil, fmt.Errorf("invalid stop timezone")
			}
		}
		if (s.Lat == nil) != (s.Lng == nil) {
			return nil, fmt.Errorf("stop requires coordinate pair")
		}
		if s.Lat != nil && !database.ValidCoordinates(*s.Lat, *s.Lng) {
			return nil, fmt.Errorf("invalid stop coordinates")
		}
		if s.LocationType <= 2 && (s.Name == "" || s.Lat == nil) {
			return nil, fmt.Errorf("stop/station/entrance requires name and position")
		}
	}
	for _, s := range d.Stops {
		if s.ParentID == "" {
			if s.LocationType >= 2 {
				return nil, fmt.Errorf("location requires parent_station")
			}
			continue
		}
		p, ok := stops[s.ParentID]
		if !ok || p.GTFSStopID == s.GTFSStopID {
			return nil, fmt.Errorf("unknown or self stop parent")
		}
		if s.LocationType == 1 {
			return nil, fmt.Errorf("station cannot have parent")
		}
		if s.LocationType == 4 && p.LocationType != 0 {
			return nil, fmt.Errorf("boarding area parent must be platform")
		}
		if s.LocationType != 4 && p.LocationType != 1 {
			return nil, fmt.Errorf("parent must be station")
		}
	}
	services := map[string]bool{}
	calSeen := map[string]bool{}
	for _, c := range d.Calendars {
		if err := unique(calSeen, c.ServiceID, "calendar"); err != nil {
			return nil, err
		}
		if c.EndDate < c.StartDate {
			return nil, fmt.Errorf("calendar dates reversed")
		}
		services[c.ServiceID] = true
	}
	exSeen := map[string]bool{}
	for _, e := range d.Exceptions {
		if err := unique(exSeen, e.ServiceID+"\x00"+e.Date, "calendar exception"); err != nil {
			return nil, err
		}
		if e.Type == 1 || calSeen[e.ServiceID] {
			services[e.ServiceID] = true
		}
	}

	trips := map[string]Trip{}
	for _, t := range d.Trips {
		if _, ok := trips[t.GTFSTripID]; ok {
			return nil, fmt.Errorf("duplicate trip_id")
		}
		trips[t.GTFSTripID] = t
		if !routes[t.GTFSRouteID] || !services[t.ServiceID] {
			return nil, fmt.Errorf("unknown trip route or service")
		}
	}
	return &MetadataIndex{Stops: stops, Trips: trips}, nil
}
func distanceValid(v *float64) bool {
	return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0)
}
func ValidateShapeGroup(ps []ShapePoint) error {
	if len(ps) < 2 {
		return fmt.Errorf("shape needs at least two points")
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].Sequence < ps[j].Sequence })
	different := false
	var distance *float64
	for i, p := range ps {
		if p.ShapeID != ps[0].ShapeID || p.Sequence < 0 || !database.ValidCoordinates(p.Lat, p.Lng) || !distanceValid(p.DistanceTraveled) {
			return fmt.Errorf("invalid shape point")
		}
		if i > 0 {
			if p.Sequence == ps[i-1].Sequence {
				return fmt.Errorf("duplicate shape sequence")
			}
			if p.Lat != ps[0].Lat || p.Lng != ps[0].Lng {
				different = true
			}
		}
		if p.DistanceTraveled != nil {
			if distance != nil && *p.DistanceTraveled < *distance {
				return fmt.Errorf("shape distance decreases")
			}
			distance = p.DistanceTraveled
		}
	}
	if !different {
		return fmt.Errorf("shape has no distinct points")
	}
	return nil
}
func (idx *MetadataIndex) ValidateStopTimeGroup(ss []StopTime) error {
	if len(ss) < 2 {
		return fmt.Errorf("trip needs at least two stop occurrences")
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].StopSequence < ss[j].StopSequence })
	if _, ok := idx.Trips[ss[0].GTFSTripID]; !ok {
		return fmt.Errorf("unknown stop_time trip")
	}
	if ss[0].ArrivalSeconds == nil || ss[len(ss)-1].ArrivalSeconds == nil {
		return fmt.Errorf("trip endpoints require times")
	}
	last := -1
	var distance *float64
	for i, s := range ss {
		stop, ok := idx.Stops[s.GTFSStopID]
		if !ok || stop.LocationType != 0 {
			return fmt.Errorf("stop_time requires known stop/platform")
		}
		if s.GTFSTripID != ss[0].GTFSTripID || s.StopSequence < 0 || !distanceValid(s.Distance) {
			return fmt.Errorf("invalid stop occurrence")
		}
		if i > 0 && s.StopSequence == ss[i-1].StopSequence {
			return fmt.Errorf("duplicate stop_time sequence")
		}
		if (s.ArrivalSeconds == nil) != (s.DepartureSeconds == nil) {
			return fmt.Errorf("arrival/departure must both be present in supported profile")
		}
		if s.ArrivalSeconds != nil {
			if *s.ArrivalSeconds < last || *s.DepartureSeconds < *s.ArrivalSeconds {
				return fmt.Errorf("trip time decreases")
			}
			last = *s.DepartureSeconds
		} else if s.Timepoint == nil || *s.Timepoint != 0 {
			return fmt.Errorf("missing exact timepoint")
		}
		if s.Distance != nil {
			if distance != nil && *s.Distance < *distance {
				return fmt.Errorf("stop_time distance decreases")
			}
			distance = s.Distance
		}
		for _, k := range []string{"location_id", "location_group_id", "start_pickup_drop_off_window", "end_pickup_drop_off_window", "pickup_booking_rule_id", "drop_off_booking_rule_id"} {
			if s.Raw[k] != "" {
				return fmt.Errorf("unsupported Schedule field %s", k)
			}
		}
	}
	return nil
}
func (idx *MetadataIndex) ValidateFrequencyGroup(fs []Frequency) error {
	if len(fs) == 0 {
		return nil
	}
	if _, ok := idx.Trips[fs[0].GTFSTripID]; !ok {
		return fmt.Errorf("unknown frequency trip")
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].StartSeconds < fs[j].StartSeconds })
	for i, f := range fs {
		if f.GTFSTripID != fs[0].GTFSTripID || f.StartSeconds < 0 || f.EndSeconds <= f.StartSeconds || f.HeadwaySecs <= 0 || (f.ExactTimes != 0 && f.ExactTimes != 1) {
			return fmt.Errorf("invalid frequency")
		}
		if i > 0 && f.StartSeconds < fs[i-1].EndSeconds {
			return fmt.Errorf("overlapping frequencies")
		}
	}
	return nil
}
func (d *Dataset) Validate() error {
	idx, err := d.ValidateMetadata()
	if err != nil {
		return err
	}
	if len(d.StopTimes) == 0 {
		return fmt.Errorf("stop_times is empty")
	}
	shapes := map[string][]ShapePoint{}
	for _, p := range d.Shapes {
		shapes[p.ShapeID] = append(shapes[p.ShapeID], p)
	}
	for _, ps := range shapes {
		if err := ValidateShapeGroup(ps); err != nil {
			return err
		}
	}
	for _, t := range d.Trips {
		if t.ShapeID != "" && len(shapes[t.ShapeID]) == 0 {
			return fmt.Errorf("unknown trip shape")
		}
	}
	times := map[string][]StopTime{}
	for _, s := range d.StopTimes {
		times[s.GTFSTripID] = append(times[s.GTFSTripID], s)
	}
	for _, ss := range times {
		if err := idx.ValidateStopTimeGroup(ss); err != nil {
			return err
		}
	}
	for id := range idx.Trips {
		if len(times[id]) == 0 {
			return fmt.Errorf("missing trip stop_times")
		}
	}
	frequencies := map[string][]Frequency{}
	for _, f := range d.Frequencies {
		frequencies[f.GTFSTripID] = append(frequencies[f.GTFSTripID], f)
	}
	for _, fs := range frequencies {
		if err := idx.ValidateFrequencyGroup(fs); err != nil {
			return err
		}
	}
	return nil
}
