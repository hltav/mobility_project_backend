package database

import (
	"database/sql"
	"strings"
	"testing"
)

func env(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DB_CONNECTION", "PGSQL_URL", "PGSQL_HOST", "PGSQL_USERNAME", "PGSQL_DATABASE", "PGSQL_PASSWORD", "PGSQL_SSLMODE", "PGSQL_ROLE"} {
		t.Setenv(k, "")
	}
}
func TestPostgresConfigurationAndDriverIsolation(t *testing.T) {
	env(t)
	t.Setenv("PGSQL_HOST", "postgres")
	t.Setenv("PGSQL_USERNAME", "fixture")
	t.Setenv("PGSQL_DATABASE", "fixture")
	t.Setenv("PGSQL_PASSWORD", "private:@/ 'value")
	c, e := ConfigFromEnv()
	if e != nil {
		t.Fatal(e)
	}
	if c.Password != "private:@/ 'value" || c.RuntimeParams["timezone"] != "UTC" {
		t.Fatal("escaped configuration lost")
	}
	for _, d := range sql.Drivers() {
		if strings.Contains(d, "mysql") {
			t.Fatal("MySQL driver registered")
		}
	}
}
func TestRejectMySQLFallbackAndRedactConfigurationErrors(t *testing.T) {
	env(t)
	t.Setenv("DB_CONNECTION", "mysql")
	t.Setenv("DB_DSN", "sensitive")
	if _, e := ConfigFromEnv(); e == nil {
		t.Fatal("MySQL accepted")
	}
	t.Setenv("DB_CONNECTION", "pgsql")
	t.Setenv("PGSQL_URL", "mysql://fixture:private-token@localhost/db")
	if _, e := ConfigFromEnv(); e == nil || strings.Contains(e.Error(), "private-token") {
		t.Fatal("invalid URL accepted or leaked")
	}
	t.Setenv("PGSQL_URL", "")
	if _, e := ConfigFromEnv(); e == nil {
		t.Fatal("DB_* implicitly used")
	}
}
