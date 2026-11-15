package gtfs

import (
	"context"
	"database/sql"
	"fmt"
	"mobility_processor/internal/gtfs/parser"
	"sort"
	"strings"
)

type SnapshotIDs struct{ Stops, Shapes, Trips, Routes map[string]int64 }

func (r *Repository) WriteMetadata(ctx context.Context, tx *sql.Tx, feed int64, d *parser.Dataset, shapeKeys map[string]bool) (*SnapshotIDs, error) {
	agencies, routes, stops, services, shapes, trips := map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}
	for _, a := range d.Agencies {
		id, err := upsert(ctx, tx, "gtfs.agencies", []string{"feed_version_id", "gtfs_agency_id", "name", "url", "timezone", "language", "raw_attributes"}, []string{"feed_version_id", "gtfs_agency_id"}, feed, optional(a.ID), a.Name, a.URL, a.Timezone, optional(a.Language), raw(a.Raw))
		if err != nil {
			return nil, err
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
			return nil, err
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
			return nil, fmt.Errorf("write GTFS stop: %w", err)
		}
		stops[v.GTFSStopID] = id
	}
	for _, v := range d.Stops {
		if v.ParentID != "" {
			if _, err := tx.ExecContext(ctx, `UPDATE gtfs.stops SET parent_gtfs_stop_pk=$1 WHERE feed_version_id=$2 AND id=$3`, stops[v.ParentID], feed, stops[v.GTFSStopID]); err != nil {
				return nil, err
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
			return nil, err
		}
		services[key] = id
	}
	for _, c := range d.Calendars {
		_, err := tx.ExecContext(ctx, `INSERT INTO gtfs.calendars(feed_version_id,service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date,raw_attributes)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(feed_version_id,service_id) DO UPDATE SET monday=EXCLUDED.monday,tuesday=EXCLUDED.tuesday,wednesday=EXCLUDED.wednesday,thursday=EXCLUDED.thursday,friday=EXCLUDED.friday,saturday=EXCLUDED.saturday,sunday=EXCLUDED.sunday,start_date=EXCLUDED.start_date,end_date=EXCLUDED.end_date,raw_attributes=EXCLUDED.raw_attributes`, feed, services[c.ServiceID], c.Days[0], c.Days[1], c.Days[2], c.Days[3], c.Days[4], c.Days[5], c.Days[6], c.StartDate, c.EndDate, raw(c.Raw))
		if err != nil {
			return nil, err
		}
	}
	for _, e := range d.Exceptions {
		_, err := upsert(ctx, tx, "gtfs.service_exceptions", []string{"feed_version_id", "service_id", "service_date", "exception_type", "raw_date", "raw_attributes"}, []string{"feed_version_id", "service_id", "service_date"}, feed, services[e.ServiceID], e.Date, e.Type, e.Raw["date"], raw(e.Raw))
		if err != nil {
			return nil, err
		}
	}

	for _, key := range sortedKeys(shapeKeys) {
		id, err := upsert(ctx, tx, "gtfs.shapes", []string{"feed_version_id", "gtfs_shape_id", "geometry_status"}, []string{"feed_version_id", "gtfs_shape_id"}, feed, key, "pending")
		if err != nil {
			return nil, err
		}
		shapes[key] = id
	}
	for _, t := range d.Trips {
		id, err := upsert(ctx, tx, "gtfs.trips", []string{"feed_version_id", "gtfs_trip_id", "route_id", "service_id", "shape_id", "gtfs_direction_id", "headsign", "trip_short_name", "block_id", "wheelchair_accessible", "bikes_allowed", "raw_attributes"}, []string{"feed_version_id", "gtfs_trip_id"}, feed, t.GTFSTripID, routes[t.GTFSRouteID], services[t.ServiceID], optionalID(shapes[t.ShapeID]), t.DirectionID, optional(t.Headsign), optional(t.ShortName), optional(t.BlockID), t.Wheelchair, t.Bikes, raw(t.Raw))
		if err != nil {
			return nil, err
		}
		trips[t.GTFSTripID] = id
	}
	return &SnapshotIDs{Stops: stops, Shapes: shapes, Trips: trips, Routes: routes}, nil
}

// Multi-row statements stay below PostgreSQL's parameter limit; table/column names are private constants.
func batchUpsert(ctx context.Context, tx *sql.Tx, table string, cols, keys []string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows)*len(cols) > 60000 {
		return fmt.Errorf("batch exceeds parameter budget")
	}
	values := make([]string, len(rows))
	args := []any{}
	updates := []string{}
	for i, row := range rows {
		if len(row) != len(cols) {
			return fmt.Errorf("batch arity mismatch")
		}
		bind := make([]string, len(row))
		for j, v := range row {
			args = append(args, v)
			bind[j] = fmt.Sprintf("$%d", len(args))
		}
		values[i] = "(" + strings.Join(bind, ",") + ")"
	}
	for _, c := range cols {
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
	_, err := tx.ExecContext(ctx, "INSERT INTO "+table+"("+strings.Join(cols, ",")+") VALUES "+strings.Join(values, ",")+" ON CONFLICT("+strings.Join(keys, ",")+") DO UPDATE SET "+strings.Join(updates, ","), args...)
	return err
}
func (r *Repository) WriteRows(ctx context.Context, tx *sql.Tx, feed int64, file string, rows []parser.Raw, ids *SnapshotIDs) error {
	// Decode one bounded batch with the exact same typed decoders as preflight.
	d, err := parser.DecodeRows(file, rows)
	if err != nil {
		return err
	}
	values := [][]any{}
	switch file {
	case "shapes.txt":
		for _, p := range d.Shapes {
			id, ok := ids.Shapes[p.ShapeID]
			if !ok {
				return fmt.Errorf("unresolved shape")
			}
			values = append(values, []any{feed, id, p.Sequence, p.Lat, p.Lng, sourceDistance(p.Raw, p.DistanceTraveled), raw(p.Raw)})
		}
		return batchUpsert(ctx, tx, "gtfs.shape_points", []string{"feed_version_id", "shape_id", "shape_point_sequence", "latitude", "longitude", "source_shape_distance", "raw_attributes"}, []string{"feed_version_id", "shape_id", "shape_point_sequence"}, values)
	case "stop_times.txt":
		for _, s := range d.StopTimes {
			trip, ok := ids.Trips[s.GTFSTripID]
			if !ok {
				return fmt.Errorf("unresolved trip")
			}
			stop, ok := ids.Stops[s.GTFSStopID]
			if !ok {
				return fmt.Errorf("unresolved stop")
			}
			values = append(values, []any{feed, trip, stop, s.StopSequence, s.ArrivalSeconds, s.DepartureSeconds, optional(s.ArrivalTime), optional(s.DepartureTime), optional(s.Headsign), s.Pickup, s.DropOff, s.Timepoint, sourceDistance(s.Raw, s.Distance), raw(s.Raw)})
		}
		return batchUpsert(ctx, tx, "gtfs.stop_times", []string{"feed_version_id", "trip_id", "gtfs_stop_pk", "stop_sequence", "arrival_service_seconds", "departure_service_seconds", "raw_arrival_time", "raw_departure_time", "stop_headsign", "pickup_type", "drop_off_type", "timepoint", "source_shape_distance", "raw_attributes"}, []string{"feed_version_id", "trip_id", "stop_sequence"}, values)
	case "frequencies.txt":
		for _, f := range d.Frequencies {
			trip, ok := ids.Trips[f.GTFSTripID]
			if !ok {
				return fmt.Errorf("unresolved frequency trip")
			}
			values = append(values, []any{feed, trip, f.StartSeconds, f.EndSeconds, f.HeadwaySecs, f.ExactTimes, optional(f.RawExactTimes), raw(f.Raw)})
		}
		return batchUpsert(ctx, tx, "gtfs.frequencies", []string{"feed_version_id", "trip_id", "start_service_seconds", "end_service_seconds", "headway_seconds", "exact_times", "raw_exact_times", "raw_attributes"}, []string{"feed_version_id", "trip_id", "start_service_seconds"}, values)
	}
	return fmt.Errorf("unknown streaming entity")
}
func (r *Repository) WritePattern(ctx context.Context, tx *sql.Tx, feed int64, t parser.Trip, seq []parser.StopTime, ids *SnapshotIDs) error {
	fingerprint := PatternFingerprint(t, seq)
	pattern, err := upsert(ctx, tx, "gtfs.journey_patterns", []string{"feed_version_id", "fingerprint_sha256", "route_id", "gtfs_direction_id", "shape_id", "headsign"}, []string{"feed_version_id", "fingerprint_sha256"}, feed, fingerprint, ids.Routes[t.GTFSRouteID], t.DirectionID, optionalID(ids.Shapes[t.ShapeID]), optional(t.Headsign))
	if err != nil {
		return err
	}
	values := [][]any{}
	for _, s := range seq {
		values = append(values, []any{feed, pattern, s.StopSequence, ids.Stops[s.GTFSStopID], s.Pickup, s.DropOff, sourceDistance(s.Raw, s.Distance)})
	}
	if err = batchUpsert(ctx, tx, "gtfs.journey_pattern_stops", []string{"feed_version_id", "pattern_id", "pattern_sequence", "gtfs_stop_pk", "pickup_type", "drop_off_type", "source_shape_distance"}, []string{"feed_version_id", "pattern_id", "pattern_sequence"}, values); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE gtfs.trips SET pattern_id=$1 WHERE feed_version_id=$2 AND id=$3`, pattern, feed, ids.Trips[t.GTFSTripID])
	return err
}
func (r *Repository) MaterializeShapes(ctx context.Context, tx *sql.Tx, feed int64, keys []string, ids *SnapshotIDs) error {
	for _, key := range keys {
		_, err := tx.ExecContext(ctx, `UPDATE gtfs.shapes SET path_geom=(SELECT ST_MakeLine(ST_SetSRID(ST_MakePoint(longitude::float8,latitude::float8),4326) ORDER BY shape_point_sequence) FROM gtfs.shape_points WHERE feed_version_id=$1 AND shape_id=$2),geometry_status='valid' WHERE feed_version_id=$1 AND id=$2`, feed, ids.Shapes[key])
		if err != nil {
			return err
		}
	}
	return nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *Repository) LoadIDs(ctx context.Context, q queryer, feed int64) (*SnapshotIDs, error) {
	ids := &SnapshotIDs{Stops: map[string]int64{}, Routes: map[string]int64{}, Shapes: map[string]int64{}, Trips: map[string]int64{}}
	for _, item := range []struct {
		table, column string
		target        map[string]int64
	}{{"stops", "gtfs_stop_id", ids.Stops}, {"routes", "gtfs_route_id", ids.Routes}, {"shapes", "gtfs_shape_id", ids.Shapes}, {"trips", "gtfs_trip_id", ids.Trips}} {
		rows, e := q.QueryContext(ctx, "SELECT id,"+item.column+" FROM gtfs."+item.table+" WHERE feed_version_id=$1", feed)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id int64
			var key string
			if e = rows.Scan(&id, &key); e != nil {
				rows.Close()
				return nil, e
			}
			item.target[key] = id
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	return ids, nil
}
