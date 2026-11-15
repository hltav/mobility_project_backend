#!/bin/sh
set -eu
fail() { printf '%s\n' "PostgreSQL test executor: $1" >&2; exit 1; }
# Only exact version diagnostics bypass connection/provisioning requirements.
[ "$#" -ne 2 ] || [ "$1" != php ] || [ "$2" != --version ] || exec "$@"
[ "${TEST_PG_EXECUTE:-0}" = 1 ] || fail 'explicit TEST_PG_EXECUTE=1 required'
[ "${DB_CONNECTION:-}" = pgsql ] || fail 'PostgreSQL driver required'
case "${PGSQL_DATABASE:-}" in
  open_transit_test|open_transit_schema_test) ;;
  open_transit) fail 'main database forbidden' ;;
  *) fail 'database outside test allowlist' ;;
esac
[ "${PGSQL_HOST:-}" = postgres ] || fail 'unexpected host'
[ "${PGSQL_PORT:-}" = 5432 ] || fail 'unexpected port'
[ "${PGSQL_SSLMODE:-}" = disable ] || fail 'unexpected isolated-cluster SSL configuration'
for key in PGSQL_URL DB_URL DB_DSN DATABASE_URL PGSQL_ROLE PGSERVICE PGSERVICEFILE PGOPTIONS PGPASSFILE PGHOST PGHOSTADDR PGPORT PGDATABASE PGUSER PGPASSWORD DB_HOST DB_PORT DB_DATABASE DB_USERNAME DB_PASSWORD; do
  eval 'value=${'"$key"':-}'
  [ -z "$value" ] || fail 'connection override forbidden'
done
[ -n "${PGSQL_USERNAME:-}" ] || fail 'test login missing'
case "$PGSQL_USERNAME" in ot_test_*) ;; *) fail 'reserved ot_test_ login required' ;; esac
[ -n "${PGSQL_PASSWORD:-}" ] || fail 'test credential missing'
[ -n "${TEST_PG_CLUSTER_ID:-}" ] || fail 'cluster identifier missing'
for file in /workspace/.env* /workspace/../.env*; do
  [ -e "$file" ] || continue
  case "$file" in */.env.example) ;; *) fail 'real environment file forbidden' ;; esac
done
[ "${APP_ENV:-}" = testing ] || fail 'Laravel testing environment required'
[ "${APP_CONFIG_CACHE:-}" = /tmp/config.php ] || fail 'unexpected Laravel configuration cache'
case "${TEST_PG_MODE:-}" in
  tests)
    [ "${POSTGRES_STAGE1_TESTS:-}" = 1 ] || fail 'Laravel test opt-in missing'
    [ "${1:-}" = php ] && [ "${2:-}" = vendor/bin/phpunit ] || fail 'only PHPUnit is permitted'
    ;;
  provision)
    [ "${TEST_PG_PROVISION:-0}" = 1 ] || fail 'explicit provisioning opt-in required'
    [ "$#" = 4 ] && [ "$1" = php ] && [ "$2" = artisan ] && [ "$3" = schema:install-postgresql ] && [ "$4" = --force ] || fail 'only explicit canonical installation is permitted'
    ;;
  *) fail 'invalid executor mode' ;;
esac
php /workspace/docker/check-test-cluster.php
mkdir -p /tmp/views
exec "$@"
