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

conf=/usr/local/share/postgresql/postgresql.conf.sample
db="${POSTGRES_DB:-postgres}"

# Replace the placeholder the Dockerfile wrote rather than appending, so a
# restart does not stack up directives.
if grep -q '^cron.database_name' "$conf"; then
  sed -i "s|^cron.database_name.*|cron.database_name = '${db}'|" "$conf"
else
  echo "cron.database_name = '${db}'" >> "$conf"
fi

exec docker-entrypoint.sh "$@"
