package stops

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mobility_processor/internal/database"
	"mobility_processor/internal/sptrans"
	"sort"
	"strconv"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

type NearbyStop struct {
	ID         int     `json:"id"`
	SptransID  int     `json:"sptrans_id"`
	Name       string  `json:"name"`
	Address    string  `json:"address"`
	Lat        float64 `json:"lat"`
	Lng        float64 `json:"lng"`
	DistanceKm float64 `json:"distance_km"`
	Lines      []int   `json:"lines,omitempty"`
}

func (r *Repository) EnsureSchema(ctx context.Context) error { return database.CheckSchema(ctx, r.db) }
func (r *Repository) UpsertStop(ctx context.Context, stop sptrans.Stop) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = upsertStop(ctx, tx, stop); err != nil {
		return err
	}
	return tx.Commit()
}
func upsertStop(ctx context.Context, tx *sql.Tx, stop sptrans.Stop) (int64, error) {
	if stop.CP <= 0 || stop.NP == "" || !database.ValidCoordinates(stop.Lat, stop.Lng) {
		return 0, errors.New("invalid SPTrans CP/name/coordinates")
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "sptrans:stop:"+strconv.Itoa(stop.CP)); err != nil {
		return 0, err
	}
	var id int64
	var status string
	err := tx.QueryRowContext(ctx, `SELECT stop_id,status FROM public.stop_source_references WHERE provider='sptrans' AND source_kind='sptrans' AND sptrans_stop_code=$1`, stop.CP).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `INSERT INTO public.bus_stops(name,address,position_geom,legacy_visible,attributes_provenance,created_at,updated_at)
  VALUES($1,NULLIF($2,''),ST_SetSRID(ST_MakePoint($4,$3),4326),true,'{"source":"sptrans"}',now(),now()) RETURNING id`, stop.NP, stop.Ed, stop.Lat, stop.Lng).Scan(&id)
	} else if err == nil {
		if status != "confirmed" {
			return 0, errors.New("existing SPTrans reference requires manual confirmation")
		}
		_, err = tx.ExecContext(ctx, `UPDATE public.bus_stops SET name=$1,address=NULLIF($2,''),position_geom=ST_SetSRID(ST_MakePoint($4,$3),4326),legacy_visible=true,updated_at=now() WHERE id=$5`, stop.NP, stop.Ed, stop.Lat, stop.Lng, id)
	}
	if err != nil {
		return 0, err
	}
	raw, _ := json.Marshal(stop)
	_, err = tx.ExecContext(ctx, `INSERT INTO public.stop_source_references(stop_id,provider,source_kind,sptrans_stop_code,external_stop_id,status,method,confidence_score,raw_attributes,created_at,updated_at)
 VALUES($1,'sptrans','sptrans',$2,$3,'confirmed','sptrans_api',1,$4,now(),now())
 ON CONFLICT(provider,sptrans_stop_code) WHERE source_kind='sptrans' DO UPDATE SET raw_attributes=EXCLUDED.raw_attributes,updated_at=now()`, id, stop.CP, strconv.Itoa(stop.CP), string(raw))
	return id, err
}
func linkStop(ctx context.Context, tx *sql.Tx, link sptrans.LineStop) (int64, error) {
	if link.LineID <= 0 || link.StopCode <= 0 {
		return 0, errors.New("invalid SPTrans CL/CP")
	}
	var lineID, stopID int64
	err := tx.QueryRowContext(ctx, `SELECT l.id,s.stop_id FROM public.lines l CROSS JOIN public.stop_source_references s
 WHERE l.provider='sptrans' AND l.sptrans_line_id=$1 AND s.provider='sptrans' AND s.source_kind='sptrans' AND s.sptrans_stop_code=$2 AND s.status='confirmed'`, link.LineID, link.StopCode).Scan(&lineID, &stopID)
	if err != nil {
		return 0, fmt.Errorf("resolve CL/CP references: %w", err)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO public.bus_stop_line(line_id,bus_stop_id,created_at,updated_at) VALUES($1,$2,now(),now()) ON CONFLICT(bus_stop_id,line_id) DO NOTHING`, lineID, stopID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO public.line_stop_sources(line_id,stop_id,source,external_line_id,external_stop_id,status,created_at,updated_at)
 VALUES($1,$2,'sptrans',$3,$4,'confirmed',now(),now())
 ON CONFLICT(line_id,stop_id,source,feed_version_id,external_line_id,external_stop_id) DO UPDATE SET status='confirmed',updated_at=now()`, lineID, stopID, strconv.Itoa(link.LineID), strconv.Itoa(link.StopCode))
	return n, err
}
func (r *Repository) UpsertLineStop(ctx context.Context, link sptrans.LineStop) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := linkStop(ctx, tx, link)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}
func (r *Repository) UpsertStopsForLine(ctx context.Context, cl int, stops []sptrans.Stop) (int64, int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var linked int64
	ordered := append([]sptrans.Stop(nil), stops...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CP < ordered[j].CP })
	for _, s := range ordered {
		if _, err := upsertStop(ctx, tx, s); err != nil {
			return 0, 0, err
		}
		n, err := linkStop(ctx, tx, sptrans.LineStop{LineID: cl, StopCode: s.CP})
		if err != nil {
			return 0, 0, err
		}
		linked += n
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return int64(len(stops)), linked, nil
}
func (r *Repository) Nearby(ctx context.Context, lat, lng, radiusKm float64, limit int, onlyWithLines bool) ([]NearbyStop, error) {
	if !database.ValidCoordinates(lat, lng) || !database.ValidRadius(radiusKm) {
		return nil, errors.New("invalid spatial query")
	}
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.db.QueryContext(ctx, `SELECT b.id,s.sptrans_stop_code,b.name,coalesce(b.address,''),ST_Y(b.position_geom),ST_X(b.position_geom),
 ST_Distance(b.position_geog,ST_SetSRID(ST_MakePoint($2,$1),4326)::geography)/1000,
 coalesce((SELECT jsonb_agg(l.sptrans_line_id ORDER BY l.display_code,l.id) FROM public.bus_stop_line p JOIN public.lines l ON l.id=p.line_id WHERE p.bus_stop_id=b.id AND l.sptrans_line_id IS NOT NULL),'[]'::jsonb)
 FROM public.bus_stops b JOIN public.stop_source_references s ON s.stop_id=b.id AND s.provider='sptrans' AND s.source_kind='sptrans' AND s.status='confirmed'
 WHERE b.legacy_visible AND ST_DWithin(b.position_geog,ST_SetSRID(ST_MakePoint($2,$1),4326)::geography,$3)
 AND (NOT $5 OR EXISTS(SELECT 1 FROM public.bus_stop_line p WHERE p.bus_stop_id=b.id)) ORDER BY 7,b.id LIMIT $4`, lat, lng, radiusKm*1000, limit, onlyWithLines)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NearbyStop{}
	for rows.Next() {
		var s NearbyStop
		var ids []byte
		if err := rows.Scan(&s.ID, &s.SptransID, &s.Name, &s.Address, &s.Lat, &s.Lng, &s.DistanceKm, &ids); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(ids, &s.Lines); err != nil {
			return nil, err
		}
		s.DistanceKm = math.Round(s.DistanceKm*1000) / 1000
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *Repository) Count(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM public.bus_stops WHERE legacy_visible`).Scan(&n)
	return n, err
}
func (r *Repository) CountLinks(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM public.bus_stop_line`).Scan(&n)
	return n, err
}
