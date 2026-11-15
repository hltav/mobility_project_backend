#!/bin/sh
set -eu
# SQL readiness belongs to the application; startup never performs migrations or imports.
exec /usr/local/bin/processor
