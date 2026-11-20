package matching

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type Repository struct {
	db   *sql.DB
	feed int64
}

func NewRepository(db *sql.DB, feed int64) *Repository { return &Repository{db: db, feed: feed} }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (r *Repository) LoadLinesToMatch(ctx context.Context) ([]LineToMatch, error) {
	return r.loadLines(ctx, r.db, 0)
}
func (r *Repository) loadLines(ctx context.Context, q queryer, lineID int64) ([]LineToMatch, error) {
	rows, err := q.QueryContext(ctx, `SELECT l.id,l.display_code,coalesce(l.headsign_primary_to_secondary,''),l.origin_lat,l.origin_lng,
 l.sptrans_line_id,l.sptrans_direction,coalesce(l.headsign,''),coalesce(l.headsign_secondary_to_primary,''),
 coalesce(a.name,''),ST_Y(a.position_geom),ST_X(a.position_geom),
 coalesce(b.name,''),ST_Y(b.position_geom),ST_X(b.position_geom),l.provider,coalesce(l.is_circular,false),
 coalesce((SELECT jsonb_agg(jsonb_build_object('position',jsonb_build_object('lat',ST_Y(bs.position_geom),'lng',ST_X(bs.position_geom)),'sequence',null) ORDER BY bs.id)
 FROM public.bus_stops bs WHERE bs.position_geom IS NOT NULL AND EXISTS (
 SELECT 1 FROM public.line_stop_sources ls JOIN public.stop_source_references sr ON sr.stop_id=ls.stop_id
 AND sr.provider='sptrans' AND sr.source_kind='sptrans' AND sr.status='confirmed'
 WHERE ls.stop_id=bs.id AND ls.line_id=l.id AND ls.source='sptrans' AND ls.status='confirmed'
 AND ls.external_line_id=l.sptrans_line_id::text AND ls.external_stop_id=sr.external_stop_id)), '[]'::jsonb)
 FROM public.lines l
 LEFT JOIN public.bus_stops a ON a.id=l.origin_stop_id
 LEFT JOIN public.bus_stops b ON b.id=l.destination_stop_id
 WHERE l.provider='sptrans' AND l.sptrans_line_id IS NOT NULL
 AND ($1::bigint=0 OR l.display_code=(SELECT display_code FROM public.lines WHERE id=$1 AND provider='sptrans'))
 ORDER BY l.id`, lineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LineToMatch{}
	for rows.Next() {
		var l LineToMatch
		var rawStops []byte
		var a, b, x, y sql.NullFloat64
		if err := rows.Scan(&l.LineID, &l.Code, &l.Origin, &l.OriginLat, &l.OriginLng,
			&l.SptransLineID, &l.Direction, &l.HeadSign, &l.SecondaryLabel, &l.StartTerminal.Name, &a, &b, &l.EndTerminal.Name, &x, &y, &l.Provider, &l.Circular, &rawStops); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawStops, &l.Stops); err != nil {
			return nil, err
		}
		l.SelectedFeedVersionID = r.feed
		l.StartTerminal.HasPosition = a.Valid && b.Valid
		l.StartTerminal.Position = ShapeEndpoint{Lat: a.Float64, Lng: b.Float64}
		l.EndTerminal.HasPosition = x.Valid && y.Valid
		l.EndTerminal.Position = ShapeEndpoint{Lat: x.Float64, Lng: y.Float64}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (r *Repository) LoadCandidates(ctx context.Context) ([]Candidate, error) {
	return r.loadCandidates(ctx, r.db)
}
func (r *Repository) loadCandidates(ctx context.Context, q queryer) ([]Candidate, error) {
	if r.feed <= 0 {
		return nil, errors.New("explicit feed version required")
	}
	rows, err := q.QueryContext(ctx, `SELECT r.id,coalesce(r.short_name,''),coalesce(r.long_name,''),p.id,p.gtfs_direction_id,
 ST_Y(ST_StartPoint(s.path_geom)),ST_X(ST_StartPoint(s.path_geom)),ST_Y(ST_EndPoint(s.path_geom)),ST_X(ST_EndPoint(s.path_geom)),
 coalesce(p.headsign,''),coalesce(first_stop.name,''),ST_Y(first_stop.position_geom),ST_X(first_stop.position_geom),
 coalesce(last_stop.name,''),ST_Y(last_stop.position_geom),ST_X(last_stop.position_geom),src.provider,r.agency_id,
 coalesce((SELECT jsonb_agg(jsonb_build_object('sequence',ps.pattern_sequence,
 'position',jsonb_build_object('lat',ST_Y(st.position_geom),'lng',ST_X(st.position_geom))) ORDER BY ps.pattern_sequence)
 FROM gtfs.journey_pattern_stops ps JOIN gtfs.stops st ON st.feed_version_id=ps.feed_version_id AND st.id=ps.gtfs_stop_pk
 WHERE ps.feed_version_id=p.feed_version_id AND ps.pattern_id=p.id AND st.position_geom IS NOT NULL),'[]'::jsonb)
 FROM gtfs.routes r JOIN gtfs.journey_patterns p ON p.feed_version_id=r.feed_version_id AND p.route_id=r.id
 LEFT JOIN gtfs.shapes s ON s.feed_version_id=p.feed_version_id AND s.id=p.shape_id AND s.geometry_status='valid'
 LEFT JOIN LATERAL (SELECT st.name,st.position_geom FROM gtfs.journey_pattern_stops ps
  JOIN gtfs.stops st ON st.feed_version_id=ps.feed_version_id AND st.id=ps.gtfs_stop_pk
  WHERE ps.feed_version_id=p.feed_version_id AND ps.pattern_id=p.id ORDER BY ps.pattern_sequence LIMIT 1) first_stop ON true
 LEFT JOIN LATERAL (SELECT st.name,st.position_geom FROM gtfs.journey_pattern_stops ps
  JOIN gtfs.stops st ON st.feed_version_id=ps.feed_version_id AND st.id=ps.gtfs_stop_pk
  WHERE ps.feed_version_id=p.feed_version_id AND ps.pattern_id=p.id ORDER BY ps.pattern_sequence DESC LIMIT 1) last_stop ON true
 JOIN ingest.feed_versions v ON v.id=r.feed_version_id AND v.state='ready'
 JOIN ingest.feed_sources src ON src.id=v.feed_source_id
 WHERE r.feed_version_id=$1 ORDER BY r.id,p.id`, r.feed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		var rawStops []byte
		var a, b, x, y, fa, fb, la, lb sql.NullFloat64
		if err := rows.Scan(&c.RouteID, &c.ShortName, &c.LongName, &c.PatternID, &c.DirectionID, &a, &b, &x, &y, &c.HeadSign, &c.StartStop.Name, &fa, &fb, &c.EndStop.Name, &la, &lb, &c.Provider, &c.AgencyID, &rawStops); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawStops, &c.Stops); err != nil {
			return nil, err
		}
		c.FeedVersionID = r.feed
		c.HasGeometry = a.Valid && b.Valid && x.Valid && y.Valid
		c.ShapeStart = ShapeEndpoint{Lat: a.Float64, Lng: b.Float64}
		c.ShapeEnd = ShapeEndpoint{Lat: x.Float64, Lng: y.Float64}
		c.StartStop = Terminal{Name: c.StartStop.Name, HasPosition: fa.Valid && fb.Valid, Position: ShapeEndpoint{Lat: fa.Float64, Lng: fb.Float64}}
		c.EndStop = Terminal{Name: c.EndStop.Name, HasPosition: la.Valid && lb.Valid, Position: ShapeEndpoint{Lat: la.Float64, Lng: lb.Float64}}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r *Repository) SaveMatches(ctx context.Context, matches []Match) error {
	if r.feed <= 0 {
		return errors.New("explicit feed required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range matches {
		if m.FeedVersionID != r.feed || m.PatternID <= 0 || m.Confidence < 0 || m.Confidence > 1 {
			return errors.New("invalid versioned match")
		}
		// Pattern and direction come from the candidate, never from SL +/- 1. Scores cannot confirm a mapping.
		res, err := tx.ExecContext(ctx, `INSERT INTO public.line_route_mappings(feed_version_id,line_id,route_id,pattern_id,gtfs_direction_id,confidence_score,matching_method,status,created_at,updated_at)
  SELECT $1,$2,$3,p.id,p.gtfs_direction_id,$5,$6,'pending',now(),now() FROM gtfs.journey_patterns p JOIN ingest.feed_versions v ON v.id=p.feed_version_id AND v.state='ready'
  WHERE p.feed_version_id=$1 AND p.route_id=$3 AND p.id=$4
  ON CONFLICT(feed_version_id,line_id,route_id,pattern_id,gtfs_direction_id) DO UPDATE SET confidence_score=EXCLUDED.confidence_score,matching_method=EXCLUDED.matching_method,updated_at=now()
  WHERE public.line_route_mappings.status='pending'`, r.feed, m.LineID, m.RouteID, m.PatternID, m.Confidence, m.MatchingMethod)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var exists bool
			err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.line_route_mappings WHERE feed_version_id=$1 AND line_id=$2 AND route_id=$3 AND pattern_id=$4 AND status IN ('confirmed','rejected'))`, r.feed, m.LineID, m.RouteID, m.PatternID).Scan(&exists)
			if err != nil {
				return err
			}
			if !exists {
				return errors.New("candidate not found in ready feed")
			}
		}
	}
	return tx.Commit()
}

// Snapshot is always read-only, consistent across operational and GTFS reads.
// A selected line includes same-code peers to detect competing directions.
func (r *Repository) Snapshot(ctx context.Context, lineID int64) ([]LineToMatch, []Candidate, error) {
	if lineID < 0 {
		return nil, nil, errors.New("invalid operational line id")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	lines, err := r.loadLines(ctx, tx, lineID)
	if err != nil {
		return nil, nil, err
	}
	candidates, err := r.loadCandidates(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	return lines, candidates, tx.Commit()
}
