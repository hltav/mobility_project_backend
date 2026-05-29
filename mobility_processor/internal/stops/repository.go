package stops

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"mobility_processor/internal/sptrans"
)

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

func (r *Repository) EnsureSchema(ctx context.Context) error {
	for _, table := range []string{"bus_stops", "bus_stop_line"} {
		exists, err := r.tableExists(ctx, table)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("tabela %q não existe; rode php artisan migrate no mobility_backend antes do sync", table)
		}
	}
	return nil
}

func (r *Repository) tableExists(ctx context.Context, table string) (bool, error) {
	var exists int
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
			AND table_name = ?
	`, table).Scan(&exists); err != nil {
		return false, fmt.Errorf("verificar tabela %q: %w", table, err)
	}
	return exists > 0, nil
}

func (r *Repository) UpsertStop(ctx context.Context, stop sptrans.Stop) error {
	if stop.CP == 0 || stop.Lat == 0 || stop.Lng == 0 {
		return nil
	}

	_, err := r.db.ExecContext(ctx,
		"INSERT INTO `bus_stops` "+
			"(sptrans_code, name, address, latitude, longitude, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, NOW(), NOW()) "+
			"ON DUPLICATE KEY UPDATE "+
			"name = VALUES(name), "+
			"address = VALUES(address), "+
			"latitude = VALUES(latitude), "+
			"longitude = VALUES(longitude), "+
			"updated_at = NOW()",
		stop.CP, stop.NP, stop.Ed, stop.Lat, stop.Lng)
	if err != nil {
		return fmt.Errorf("upsert parada cp=%d: %w", stop.CP, err)
	}
	return nil
}

type GTFSStop struct {
	Code    int
	Name    string
	Address string
	Lat     float64
	Lng     float64
}

func (r *Repository) UpsertGTFSStop(ctx context.Context, stop GTFSStop) error {
	if stop.Code == 0 || stop.Lat == 0 || stop.Lng == 0 {
		return nil
	}

	_, err := r.db.ExecContext(ctx,
		"INSERT INTO `bus_stops` "+
			"(sptrans_code, name, address, latitude, longitude, created_at, updated_at) "+
			"VALUES (?, ?, ?, ?, ?, NOW(), NOW()) "+
			"ON DUPLICATE KEY UPDATE "+
			"name = VALUES(name), "+
			"address = COALESCE(NULLIF(VALUES(address), ''), address), "+
			"latitude = VALUES(latitude), "+
			"longitude = VALUES(longitude), "+
			"updated_at = NOW()",
		stop.Code, stop.Name, stop.Address, stop.Lat, stop.Lng)
	if err != nil {
		return fmt.Errorf("upsert parada GTFS code=%d: %w", stop.Code, err)
	}
	return nil
}

func (r *Repository) UpsertLineStop(ctx context.Context, link sptrans.LineStop) (int64, error) {
	if link.LineID == 0 || link.StopCode == 0 {
		return 0, nil
	}

	result, err := r.db.ExecContext(ctx,
		"INSERT IGNORE INTO `bus_stop_line` (line_id, bus_stop_id, created_at, updated_at) "+
			"SELECT l.id, bs.id, NOW(), NOW() "+
			"FROM `lines` l "+
			"JOIN `bus_stops` bs ON bs.sptrans_code = ? "+
			"WHERE l.sptrans_id = ?",
		link.StopCode, link.LineID)
	if err != nil {
		return 0, fmt.Errorf("vincular linha %d à parada %d: %w", link.LineID, link.StopCode, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("contar vínculo linha %d à parada %d: %w", link.LineID, link.StopCode, err)
	}
	return rows, nil
}

func (r *Repository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `bus_stops`").Scan(&count); err != nil {
		return 0, fmt.Errorf("contar paradas: %w", err)
	}
	return count, nil
}

func (r *Repository) CountLinks(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `bus_stop_line`").Scan(&count); err != nil {
		return 0, fmt.Errorf("contar vínculos linha-parada: %w", err)
	}
	return count, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
