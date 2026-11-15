package lines

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"mobility_processor/internal/database"
	"mobility_processor/internal/sptrans"
	"strings"
)

type Line struct {
	ID          int
	Code        string
	Name        string
	Origin      string
	Destination string
	Circular    bool
	SptransID   int
	OriginLat   sql.NullFloat64
	OriginLng   sql.NullFloat64
}

type NearbyLine struct {
	ID          int     `json:"id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Origin      string  `json:"origin"`
	Destination string  `json:"destination"`
	Circular    bool    `json:"circular"`
	SptransID   int     `json:"sptrans_id"`
	OriginLat   float64 `json:"origin_lat"`
	OriginLng   float64 `json:"origin_lng"`
	DistanceKm  float64 `json:"distance_km"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

var ErrAmbiguousLine = errors.New("line code has multiple SPTrans identities; specify SL")

func (r *Repository) UpsertFromSPTrans(ctx context.Context, raw sptrans.RawLine) error {
	if raw.CL <= 0 || strings.TrimSpace(raw.LT) == "" || raw.TL < 0 || (raw.SL != 1 && raw.SL != 2) {
		return errors.New("invalid SPTrans CL/LT/TL/SL")
	}
	line := FromSPTrans(raw)
	_, err := r.db.ExecContext(ctx, `INSERT INTO public.lines
 (provider,sptrans_line_id,code_prefix,code_suffix,display_code,display_name,sptrans_direction,is_circular,headsign_primary_to_secondary,headsign_secondary_to_primary,created_at,updated_at)
 VALUES ('sptrans',$1,$2,$3,$4,$5,$6,$7,$8,$9,now(),now())
 ON CONFLICT(provider,sptrans_line_id) WHERE sptrans_line_id IS NOT NULL DO UPDATE SET
 code_prefix=EXCLUDED.code_prefix,code_suffix=EXCLUDED.code_suffix,display_code=EXCLUDED.display_code,display_name=EXCLUDED.display_name,
 sptrans_direction=EXCLUDED.sptrans_direction,is_circular=EXCLUDED.is_circular,headsign_primary_to_secondary=EXCLUDED.headsign_primary_to_secondary,
 headsign_secondary_to_primary=EXCLUDED.headsign_secondary_to_primary,updated_at=now()`, raw.CL, raw.LT, raw.TL, line.Code, line.Name, raw.SL, raw.LC, raw.TP, raw.TS)
	return err
}

const projection = `id,display_code,display_name,coalesce(headsign_primary_to_secondary,''),coalesce(headsign_secondary_to_primary,''),coalesce(is_circular,false),sptrans_line_id,origin_lat,origin_lng`

func scanLine(row interface{ Scan(...any) error }, l *Line) error {
	return row.Scan(&l.ID, &l.Code, &l.Name, &l.Origin, &l.Destination, &l.Circular, &l.SptransID, &l.OriginLat, &l.OriginLng)
}
func (r *Repository) LinesForGeocoding(ctx context.Context, fresh bool, limit int) ([]Line, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+projection+` FROM public.lines WHERE sptrans_line_id IS NOT NULL AND coalesce(headsign_primary_to_secondary,'')<>'' AND ($1 OR origin_lat IS NULL OR origin_lng IS NULL) ORDER BY id LIMIT NULLIF($2,0)`, fresh, max(limit, 0))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Line{}
	for rows.Next() {
		var l Line
		if err := scanLine(rows, &l); err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	return result, rows.Err()
}
func (r *Repository) UpdateOriginCoordinates(ctx context.Context, id int, lat, lng float64) error {
	if !database.ValidCoordinates(lat, lng) {
		return errors.New("invalid coordinates")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE public.lines SET origin_lat=$1,origin_lng=$2,updated_at=now() WHERE id=$3`, lat, lng, id)
	return err
}
func (r *Repository) UpdateOriginCoordinatesByOrigin(ctx context.Context, origin string, lat, lng float64) (int64, error) {
	if !database.ValidCoordinates(lat, lng) {
		return 0, errors.New("invalid coordinates")
	}
	res, err := r.db.ExecContext(ctx, `UPDATE public.lines SET origin_lat=$1,origin_lng=$2,updated_at=now() WHERE headsign_primary_to_secondary=$3 AND (origin_lat IS NULL OR origin_lng IS NULL)`, lat, lng, origin)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
func (r *Repository) Nearby(ctx context.Context, lat, lng, radiusKm float64) ([]NearbyLine, error) {
	if !database.ValidCoordinates(lat, lng) || !database.ValidRadius(radiusKm) {
		return nil, errors.New("invalid spatial query")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+projection+`,ST_Distance(ST_SetSRID(ST_MakePoint(origin_lng::float8,origin_lat::float8),4326)::geography,ST_SetSRID(ST_MakePoint($2,$1),4326)::geography)/1000
 FROM public.lines WHERE sptrans_line_id IS NOT NULL AND origin_lat IS NOT NULL AND origin_lng IS NOT NULL
 AND ST_DWithin(ST_SetSRID(ST_MakePoint(origin_lng::float8,origin_lat::float8),4326)::geography,ST_SetSRID(ST_MakePoint($2,$1),4326)::geography,$3)
 ORDER BY 10,id`, lat, lng, radiusKm*1000)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NearbyLine{}
	for rows.Next() {
		var l NearbyLine
		if err := rows.Scan(&l.ID, &l.Code, &l.Name, &l.Origin, &l.Destination, &l.Circular, &l.SptransID, &l.OriginLat, &l.OriginLng, &l.DistanceKm); err != nil {
			return nil, err
		}
		l.DistanceKm = math.Round(l.DistanceKm*1000) / 1000
		out = append(out, l)
	}
	return out, rows.Err()
}
func (r *Repository) ResolveSptransID(ctx context.Context, code string, direction int) (int, error) {
	if direction < 0 || direction > 2 {
		return 0, errors.New("SPTrans direction must be 1 or 2; 0 means unspecified")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT sptrans_line_id FROM public.lines WHERE provider='sptrans' AND display_code=$1 AND sptrans_line_id IS NOT NULL AND ($2=0 OR sptrans_direction=$2) ORDER BY id LIMIT 2`, code, direction)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) > 1 {
		return 0, ErrAmbiguousLine
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}
func (r *Repository) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM public.lines`).Scan(&n)
	return n, err
}
func FromSPTrans(raw sptrans.RawLine) Line {
	return Line{Code: fmt.Sprintf("%s-%d", strings.TrimSpace(raw.LT), raw.TL), Name: strings.TrimSpace(raw.TP + " - " + raw.TS), Origin: raw.TP, Destination: raw.TS, Circular: raw.LC, SptransID: raw.CL}
}
