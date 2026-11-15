// Package database owns PostgreSQL configuration and the process-wide SQL pool.
// Repositories borrow the pool; only the composition root closes it.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func ConfigFromEnv() (*pgx.ConnConfig, error) {
	engine := os.Getenv("DB_CONNECTION")
	if engine != "" && engine != "pgsql" && engine != "postgres" {
		return nil, errors.New("Processor requires DB_CONNECTION=pgsql; MySQL is not supported")
	}
	raw := os.Getenv("PGSQL_URL")
	if raw == "" {
		host, user, name := os.Getenv("PGSQL_HOST"), os.Getenv("PGSQL_USERNAME"), os.Getenv("PGSQL_DATABASE")
		if host == "" || user == "" || name == "" {
			return nil, errors.New("configure PGSQL_HOST, PGSQL_USERNAME and PGSQL_DATABASE (or PGSQL_URL)")
		}
		port := os.Getenv("PGSQL_PORT")
		if port == "" {
			port = "5432"
		}
		ssl := os.Getenv("PGSQL_SSLMODE")
		if ssl == "" {
			ssl = "prefer"
		}
		u := url.URL{Scheme: "postgresql", User: url.UserPassword(user, os.Getenv("PGSQL_PASSWORD")), Host: net.JoinHostPort(host, port), Path: "/" + name}
		q := url.Values{"sslmode": {ssl}}
		u.RawQuery = q.Encode()
		raw = u.String()
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return nil, errors.New("invalid PGSQL_URL; PostgreSQL URL required (credentials omitted)")
	}
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration (credentials omitted)")
	}
	cfg.ConnectTimeout = 5 * time.Second
	cfg.RuntimeParams["timezone"] = "UTC"
	cfg.RuntimeParams["application_name"] = "mobility_processor"
	if role := os.Getenv("PGSQL_ROLE"); role != "" {
		if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(role) {
			return nil, errors.New("invalid PGSQL_ROLE")
		}
		cfg.RuntimeParams["role"] = role
	}
	return cfg, nil
}

func OpenFromEnv(ctx context.Context) (*sql.DB, error) {
	cfg, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	ping, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = db.PingContext(ping); err != nil {
		db.Close()
		return nil, errors.New("PostgreSQL connection failed (connection details omitted)")
	}
	return db, nil
}

func CheckSchema(ctx context.Context, db *sql.DB) error {
	var ready bool
	err := db.QueryRowContext(ctx, `SELECT to_regclass('public.lines') IS NOT NULL AND to_regclass('public.stop_source_references') IS NOT NULL AND to_regclass('gtfs.stop_times') IS NOT NULL AND to_regprocedure('ingest.promote_feed(bigint,bigint,bigint,text,text)') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_extension WHERE extname='postgis')`).Scan(&ready)
	if err != nil {
		return fmt.Errorf("check PostgreSQL schema: %w", err)
	}
	if !ready {
		return errors.New("canonical schema missing; run the explicit Laravel PostgreSQL installer")
	}
	return nil
}

func ValidCoordinates(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}
func ValidRadius(km float64) bool { return !math.IsNaN(km) && !math.IsInf(km, 0) && km > 0 }
