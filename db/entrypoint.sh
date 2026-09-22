#!/usr/bin/env bash
#
# Sets cron.database_name from POSTGRES_DB before handing over to the stock
# PostgreSQL entrypoint.
#
# It cannot be baked into the image: pg_cron reads job descriptions from one
# database, named at server start, and CREATE EXTENSION pg_cron refuses to run
# anywhere else. A value fixed at build time would therefore have to match
# whatever POSTGRES_DB a deployment happens to use, and when it does not the
# container does not merely lose its scheduled flips -- init.sql fails and the
# container exits.
#
# Writing it here keeps the image self-sufficient: `docker run -e
# POSTGRES_DB=anything` works without the caller having to know that
# `-c cron.database_name=...` is required.
set -euo pipefail

db="${POSTGRES_DB:-postgres}"

# Appended as a server argument rather than edited into postgresql.conf.
#
# The obvious `sed -i "s|...|cron.database_name = '$db'|"` breaks on the very
# names it is meant to carry: PostgreSQL accepts a database called `a|b`, and
# the pipe then terminates sed's substitution -- the container exits before
# PostgreSQL starts. `&` and `\` are mangled more quietly still. Passing the
# value as an argument hands it to PostgreSQL verbatim, with no second layer
# of syntax to escape.
if [ "${1:-}" = "postgres" ]; then
  # Only when the caller has not set it already, so an explicit
  # `-c cron.database_name=...` in a compose file still wins.
  for arg in "$@"; do
    case "$arg" in
      cron.database_name=*) exec docker-entrypoint.sh "$@" ;;
    esac
  done
  set -- "$@" -c "cron.database_name=${db}"
fi

exec docker-entrypoint.sh "$@"
