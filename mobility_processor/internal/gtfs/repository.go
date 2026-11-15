package gtfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mobility_processor/internal/gtfs/parser"
	"sort"
	"strings"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }
func raw(v any) string                     { b, _ := json.Marshal(v); return string(b) }
func optional(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func optionalID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// Identifiers are private constants from the calls below; source values are always bound.
func upsert(ctx context.Context, tx *sql.Tx, table string, cols, keys []string, values ...any) (int64, error) {
	args := make([]string, len(cols))
	updates := []string{}
	for i, c := range cols {
		args[i] = fmt.Sprintf("$%d", i+1)
		key := false
		for _, k := range keys {
			if c == k {
				key = true
			}
		}
		if !key {
			updates = append(updates, c+"=EXCLUDED."+c)
		}
	}
	var id int64
	err := tx.QueryRowContext(ctx, "INSERT INTO "+table+" ("+strings.Join(cols, ",")+") VALUES ("+strings.Join(args, ",")+") ON CONFLICT ("+strings.Join(keys, ",")+") DO UPDATE SET "+strings.Join(updates, ",")+" RETURNING id", values...).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", table, err)
	}
	return id, nil
}
func (r *Repository) WriteSnapshot(ctx context.Context, tx *sql.Tx, feed int64, d *parser.Dataset) error {
	if err := d.Validate(); err != nil {
		return err
	}
	agencies, routes, stops, services, shapes, trips := map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}
	for _, a := range d.Agencies {
		id, err := upsert(ctx, tx, "gtfs.agencies", []string{"feed_version_id", "gtfs_agency_id", "name", "url", "timezone", "language", "raw_attributes"}, []string{"feed_version_id", "gtfs_agency_id"}, feed, optional(a.ID), a.Name, a.URL, a.Timezone, optional(a.Language), raw(a.Raw))
		if err != nil {
			return err
		}
		agencies[a.ID] = id
	}
	for _, v := range d.Routes {
		agency := agencies[v.AgencyID]
		if v.AgencyID == "" && len(agencies) == 1 {
			for _, id := range agencies {
				agency = id
			}
		}
		id, err := upsert(ctx, tx, "gtfs.routes", []string{"feed_version_id", "gtfs_route_id", "agency_id", "short_name", "long_name", "route_type", "color_hex", "text_color_hex", "raw_attributes"}, []string{"feed_version_id", "gtfs_route_id"}, feed, v.GTFSRouteID, agency, optional(v.ShortName), optional(v.LongName), v.RouteType, optional(v.Color), optional(v.TextColor), raw(v.Raw))
		if err != nil {
			return err
		}
		routes[v.GTFSRouteID] = id
	}
	orderedStops := append([]parser.Stop(nil), d.Stops...)
	rank := func(kind int) int {
		if kind == 1 {
			return 0
		}
		if kind == 4 {
			return 2
		}
		return 1
	}
	sort.SliceStable(orderedStops, func(i, j int) bool { return rank(orderedStops[i].LocationType) < rank(orderedStops[j].LocationType) })
	for _, v := range orderedStops {
		var id int64
		err := tx.QueryRowContext(ctx, `INSERT INTO gtfs.stops(feed_version_id,gtfs_stop_id,stop_code,name,description,location_type,timezone,zone_id,wheelchair_boarding,position_geom,raw_attributes,parent_gtfs_stop_pk)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,CASE WHEN $10::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($11,$10),4326) END,$12,$13)
  ON CONFLICT(feed_version_id,gtfs_stop_id) DO UPDATE SET parent_gtfs_stop_pk=EXCLUDED.parent_gtfs_stop_pk,stop_code=EXCLUDED.stop_code,name=EXCLUDED.name,description=EXCLUDED.description,location_type=EXCLUDED.location_type,timezone=EXCLUDED.timezone,zone_id=EXCLUDED.zone_id,wheelchair_boarding=EXCLUDED.wheelchair_boarding,position_geom=EXCLUDED.position_geom,raw_attributes=EXCLUDED.raw_attributes RETURNING id`, feed, v.GTFSStopID, optional(v.Code), optional(v.Name), optional(v.Description), v.LocationType, optional(v.Timezone), optional(v.ZoneID), v.Wheelchair, v.Lat, v.Lng, raw(v.Raw), optionalID(stops[v.ParentID])).Scan(&id)
		if err != nil {
			return fmt.Errorf("write GTFS stop: %w", err)
		}
		stops[v.GTFSStopID] = id
	}
	for _, v := range d.Stops {
		if v.ParentID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE gtfs.stops SET parent_gtfs_stop_pk=$1 WHERE feed_version_id=$2 AND id=$3`, stops[v.ParentID], feed, stops[v.GTFSStopID]); err != nil {
				return err
			}
		}
	}
	weekly := map[string]bool{}
	for _, c := range d.Calendars {
		weekly[c.ServiceID] = true
	}
	for _, e := range d.Exceptions {
		if _, ok := weekly[e.ServiceID]; !ok {
			weekly[e.ServiceID] = false
		}
	}
	for _, key := range sortedKeys(weekly) {
		id, err := upsert(ctx, tx, "gtfs.services", []string{"feed_version_id", "gtfs_service_id", "has_weekly_calendar"}, []string{"feed_version_id", "gtfs_service_id"}, feed, key, weekly[key])
		if err != nil {
			return err
		}
		services[key] = id
	}
	for _, c := range d.Calendars {
		_, err := tx.ExecContext(ctx, `INSERT INTO gtfs.calendars(feed_version_id,service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date,raw_attributes)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(feed_version_id,service_id) DO UPDATE SET monday=EXCLUDED.monday,tuesday=EXCLUDED.tuesday,wednesday=EXCLUDED.wednesday,thursday=EXCLUDED.thursday,friday=EXCLUDED.friday,saturday=EXCLUDED.saturday,sunday=EXCLUDED.sunday,start_date=EXCLUDED.start_date,end_date=EXCLUDED.end_date,raw_attributes=EXCLUDED.raw_attributes`, feed, services[c.ServiceID], c.Days[0], c.Days[1], c.Days[2], c.Days[3], c.Days[4], c.Days[5], c.Days[6], c.StartDate, c.EndDate, raw(c.Raw))
		if err != nil {
			return err
		}
	}
	for _, e := range d.Exceptions {
		_, err := upsert(ctx, tx, "gtfs.service_exceptions", []string{"feed_version_id", "service_id", "service_date", "exception_type", "raw_date", "raw_attributes"}, []string{"feed_version_id", "service_id", "service_date"}, feed, services[e.ServiceID], e.Date, e.Type, e.Raw["date"], raw(e.Raw))
		if err != nil {
			return err
		}
	}
	for _, p := range d.Shapes {
		if shapes[p.ShapeID] == 0 {
			id, err := upsert(ctx, tx, "gtfs.shapes", []string{"feed_version_id", "gtfs_shape_id", "geometry_status"}, []string{"feed_version_id", "gtfs_shape_id"}, feed, p.ShapeID, "pending")
			if err != nil {
				return err
			}
			shapes[p.ShapeID] = id
		}
		_, err := upsert(ctx, tx, "gtfs.shape_points", []string{"feed_version_id", "shape_id", "shape_point_sequence", "latitude", "longitude", "source_shape_distance", "raw_attributes"}, []string{"feed_version_id", "shape_id", "shape_point_sequence"}, feed, shapes[p.ShapeID], p.Sequence, p.Lat, p.Lng, sourceDistance(p.Raw, p.DistanceTraveled), raw(p.Raw))
		if err != nil {
			return err
		}
	}
	for _, id := range shapes {
		_, err := tx.ExecContext(ctx, `UPDATE gtfs.shapes SET path_geom=(SELECT ST_MakeLine(ST_SetSRID(ST_MakePoint(longitude::float8,latitude::float8),4326) ORDER BY shape_point_sequence) FROM gtfs.shape_points WHERE feed_version_id=$1 AND shape_id=$2),geometry_status='valid' WHERE feed_version_id=$1 AND id=$2`, feed, id)
		if err != nil {
			return err
		}
	}
	times := map[string][]parser.StopTime{}
	for _, s := range d.StopTimes {
		times[s.GTFSTripID] = append(times[s.GTFSTripID], s)
	}
	for _, t := range d.Trips {
		seq := times[t.GTFSTripID]
		sort.Slice(seq, func(i, j int) bool { return seq[i].StopSequence < seq[j].StopSequence })
		fingerprint := PatternFingerprint(t, seq)
		pattern, err := upsert(ctx, tx, "gtfs.journey_patterns", []string{"feed_version_id", "fingerprint_sha256", "route_id", "gtfs_direction_id", "shape_id", "headsign"}, []string{"feed_version_id", "fingerprint_sha256"}, feed, fingerprint, routes[t.GTFSRouteID], t.DirectionID, optionalID(shapes[t.ShapeID]), optional(t.Headsign))
		if err != nil {
			return err
		}
		for _, s := range seq {
			_, err := upsert(ctx, tx, "gtfs.journey_pattern_stops", []string{"feed_version_id", "pattern_id", "pattern_sequence", "gtfs_stop_pk", "pickup_type", "drop_off_type", "source_shape_distance"}, []string{"feed_version_id", "pattern_id", "pattern_sequence"}, feed, pattern, s.StopSequence, stops[s.GTFSStopID], s.Pickup, s.DropOff, sourceDistance(s.Raw, s.Distance))
			if err != nil {
				return err
			}
		}
		id, err := upsert(ctx, tx, "gtfs.trips", []string{"feed_version_id", "gtfs_trip_id", "route_id", "service_id", "pattern_id", "shape_id", "gtfs_direction_id", "headsign", "trip_short_name", "block_id", "wheelchair_accessible", "bikes_allowed", "raw_attributes"}, []string{"feed_version_id", "gtfs_trip_id"}, feed, t.GTFSTripID, routes[t.GTFSRouteID], services[t.ServiceID], pattern, optionalID(shapes[t.ShapeID]), t.DirectionID, optional(t.Headsign), optional(t.ShortName), optional(t.BlockID), t.Wheelchair, t.Bikes, raw(t.Raw))
		if err != nil {
			return err
		}
		trips[t.GTFSTripID] = id
	}
	for _, s := range d.StopTimes {
		_, err := upsert(ctx, tx, "gtfs.stop_times", []string{"feed_version_id", "trip_id", "gtfs_stop_pk", "stop_sequence", "arrival_service_seconds", "departure_service_seconds", "raw_arrival_time", "raw_departure_time", "stop_headsign", "pickup_type", "drop_off_type", "timepoint", "source_shape_distance", "raw_attributes"}, []string{"feed_version_id", "trip_id", "stop_sequence"}, feed, trips[s.GTFSTripID], stops[s.GTFSStopID], s.StopSequence, s.ArrivalSeconds, s.DepartureSeconds, optional(s.ArrivalTime), optional(s.DepartureTime), optional(s.Headsign), s.Pickup, s.DropOff, s.Timepoint, sourceDistance(s.Raw, s.Distance), raw(s.Raw))
		if err != nil {
			return err
		}
	}
	for _, f := range d.Frequencies {
		_, err := upsert(ctx, tx, "gtfs.frequencies", []string{"feed_version_id", "trip_id", "start_service_seconds", "end_service_seconds", "headway_seconds", "exact_times", "raw_exact_times", "raw_attributes"}, []string{"feed_version_id", "trip_id", "start_service_seconds"}, feed, trips[f.GTFSTripID], f.StartSeconds, f.EndSeconds, f.HeadwaySecs, f.ExactTimes, optional(f.RawExactTimes), raw(f.Raw))
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	return err
}
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func PatternFingerprint(t parser.Trip, seq []parser.StopTime) string {
	return parser.PatternFingerprint(t, seq)
}

// Validate numerically, but bind the original decimal to PostgreSQL numeric without a float round trip.
func sourceDistance(r parser.Raw, v *float64) any {
	if v == nil {
		return nil
	}
	if s := r["shape_dist_traveled"]; s != "" {
		return s
	}
	return *v
}
