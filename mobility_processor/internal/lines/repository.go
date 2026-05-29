package lines

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"mobility_processor/internal/sptrans"
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

type Repository struct {
	db *sql.DB
}

func OpenFromEnv() (*Repository, error) {
	connection := env("DB_CONNECTION", "mysql")
	if connection != "mysql" {
		return nil, fmt.Errorf("DB_CONNECTION=%q não suportado pelo processor Go; use mysql", connection)
	}

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		host := env("DB_HOST", "127.0.0.1")
		port := env("DB_PORT", "3306")
		database := os.Getenv("DB_DATABASE")
		username := os.Getenv("DB_USERNAME")
		password := os.Getenv("DB_PASSWORD")
		if database == "" || username == "" {
			return nil, fmt.Errorf("defina DB_DATABASE e DB_USERNAME ou informe DB_DSN")
		}

		params := url.Values{}
		params.Set("charset", "utf8mb4")
		params.Set("collation", "utf8mb4_unicode_ci")
		params.Set("parseTime", "true")
		params.Set("loc", "Local")

		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s", username, password, host, port, database, params.Encode())
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir conexão MySQL: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar no MySQL: %w", err)
	}

	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) UpsertFromSPTrans(ctx context.Context, raw sptrans.RawLine) error {
	line := FromSPTrans(raw)
	if line.SptransID == 0 {
		return nil
	}

	_, err := r.db.ExecContext(ctx,
		"INSERT INTO `lines` "+
			"(code, name, origin, destination, circular, sptrans_id, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, ?, NOW(), NOW()) "+
			"ON DUPLICATE KEY UPDATE "+
			"code = VALUES(code), "+
			"name = VALUES(name), "+
			"origin = VALUES(origin), "+
			"destination = VALUES(destination), "+
			"circular = VALUES(circular), "+
			"updated_at = NOW()",
		line.Code, line.Name, line.Origin, line.Destination, line.Circular, line.SptransID)
	if err != nil {
		return fmt.Errorf("upsert linha sptrans_id=%d: %w", line.SptransID, err)
	}
	return nil
}

func (r *Repository) LinesForGeocoding(ctx context.Context, fresh bool, limit int) ([]Line, error) {
	query := "SELECT id, code, name, origin, destination, circular, sptrans_id, origin_lat, origin_lng " +
		"FROM `lines` " +
		"WHERE origin <> ''"
	if !fresh {
		query += " AND (origin_lat IS NULL OR origin_lng IS NULL)"
	}
	query += " ORDER BY id"
	if limit > 0 {
		query += " LIMIT " + strconv.Itoa(limit)
	}

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("buscar linhas para geocoding: %w", err)
	}
	defer rows.Close()

	var result []Line
	for rows.Next() {
		var line Line
		if err := rows.Scan(
			&line.ID,
			&line.Code,
			&line.Name,
			&line.Origin,
			&line.Destination,
			&line.Circular,
			&line.SptransID,
			&line.OriginLat,
			&line.OriginLng,
		); err != nil {
			return nil, fmt.Errorf("ler linha: %w", err)
		}
		result = append(result, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar linhas: %w", err)
	}
	return result, nil
}

func (r *Repository) UpdateOriginCoordinates(ctx context.Context, id int, lat, lng float64) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE `lines` "+
			"SET origin_lat = ?, origin_lng = ?, updated_at = NOW() "+
			"WHERE id = ?",
		lat, lng, id)
	if err != nil {
		return fmt.Errorf("atualizar coordenadas da linha id=%d: %w", id, err)
	}
	return nil
}

func (r *Repository) UpdateOriginCoordinatesByOrigin(ctx context.Context, origin string, lat, lng float64) (int64, error) {
	result, err := r.db.ExecContext(ctx,
		"UPDATE `lines` "+
			"SET origin_lat = ?, origin_lng = ?, updated_at = NOW() "+
			"WHERE origin = ? AND (origin_lat IS NULL OR origin_lng IS NULL)",
		lat, lng, origin)
	if err != nil {
		return 0, fmt.Errorf("atualizar coordenadas da origem %q: %w", origin, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("contar linhas atualizadas da origem %q: %w", origin, err)
	}
	return rows, nil
}

func (r *Repository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `lines`").Scan(&count); err != nil {
		return 0, fmt.Errorf("contar linhas: %w", err)
	}
	return count, nil
}

func FromSPTrans(raw sptrans.RawLine) Line {
	origin := strings.TrimSpace(raw.TP)
	destination := strings.TrimSpace(raw.TS)
	name := strings.TrimSpace(origin + " - " + destination)
	return Line{
		Code:        strings.TrimSpace(raw.LT),
		Name:        name,
		Origin:      origin,
		Destination: destination,
		Circular:    raw.LC,
		SptransID:   raw.CL,
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
