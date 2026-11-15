package matching

import (
	"context"
	"database/sql"
	"errors"
)

type Repository struct {
	db   *sql.DB
	feed int64
}

func NewRepository(db *sql.DB, feed int64) *Repository { return &Repository{db: db, feed: feed} }
func (r *Repository) LoadLinesToMatch(ctx context.Context) ([]LineToMatch, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,display_code,coalesce(headsign_primary_to_secondary,''),origin_lat,origin_lng FROM public.lines WHERE provider='sptrans' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LineToMatch{}
	for rows.Next() {
		var l LineToMatch
		if err := rows.Scan(&l.LineID, &l.Code, &l.Origin, &l.OriginLat, &l.OriginLng); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (r *Repository) LoadCandidates(ctx context.Context) ([]Candidate, error) {
	if r.feed <= 0 {
		return nil, errors.New("explicit feed version required")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.id,coalesce(r.short_name,''),coalesce(r.long_name,''),p.id,p.gtfs_direction_id,
 ST_Y(ST_StartPoint(s.path_geom)),ST_X(ST_StartPoint(s.path_geom)),ST_Y(ST_EndPoint(s.path_geom)),ST_X(ST_EndPoint(s.path_geom))
 FROM gtfs.routes r JOIN gtfs.journey_patterns p ON p.feed_version_id=r.feed_version_id AND p.route_id=r.id
 LEFT JOIN gtfs.shapes s ON s.feed_version_id=p.feed_version_id AND s.id=p.shape_id
 JOIN ingest.feed_versions v ON v.id=r.feed_version_id AND v.state='ready'
 WHERE r.feed_version_id=$1 ORDER BY r.id,p.id`, r.feed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var c Candidate
		var a, b, x, y sql.NullFloat64
		if err := rows.Scan(&c.RouteID, &c.ShortName, &c.LongName, &c.PatternID, &c.DirectionID, &a, &b, &x, &y); err != nil {
			return nil, err
		}
		c.FeedVersionID = r.feed
		c.HasGeometry = a.Valid && b.Valid && x.Valid && y.Valid
		c.ShapeStart = ShapeEndpoint{Lat: a.Float64, Lng: b.Float64}
		c.ShapeEnd = ShapeEndpoint{Lat: x.Float64, Lng: y.Float64}
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
