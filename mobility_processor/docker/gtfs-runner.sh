#!/bin/sh
set -eu
# Offline inspection requires no secret. Imports receive a dedicated restricted-login secret file.
if [ -n "${PGSQL_PASSWORD_FILE:-}" ]; then
    export PGSQL_PASSWORD="$(cat "$PGSQL_PASSWORD_FILE")"
fi
exec "$@"
