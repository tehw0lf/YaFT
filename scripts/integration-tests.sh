#!/usr/bin/env bash
# Run the integration suite against a real PostgreSQL with pg_cron.
#
# Generates a throwaway database password for this run, so no credential is
# written down in the repository, and tears the stack down afterwards.
#
# Usage: scripts/integration-tests.sh [extra go test args...]
set -euo pipefail

cd "$(dirname "$0")/.."

# --env-file /dev/null keeps Compose from resolving ${POSTGRES_PASSWORD} out of
# the repository's .env, which holds the developer's real database password.
COMPOSE=(docker compose --env-file /dev/null -f docker-compose-test.yml)

if [[ -z "${POSTGRES_PASSWORD:-}" ]]; then
    if command -v openssl >/dev/null 2>&1; then
        POSTGRES_PASSWORD="$(openssl rand -hex 16)"
    else
        POSTGRES_PASSWORD="$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
    export POSTGRES_PASSWORD
fi

cleanup() {
    "${COMPOSE[@]}" down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

# A stack left over from an earlier run still holds the previous password in
# its volume, so start from a clean slate.
cleanup

"${COMPOSE[@]}" up -d --build testdb

echo "Waiting for PostgreSQL..."
for _ in $(seq 1 60); do
    if "${COMPOSE[@]}" exec -T testdb pg_isready -U yaft -d yaft >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

if ! "${COMPOSE[@]}" exec -T testdb pg_isready -U yaft -d yaft >/dev/null 2>&1; then
    echo "Database did not become ready" >&2
    "${COMPOSE[@]}" logs testdb >&2
    exit 1
fi

if [[ $# -gt 0 ]]; then
    "${COMPOSE[@]}" run --rm tests go test -tags=integration -v -count=1 -timeout=15m "$@"
else
    "${COMPOSE[@]}" run --rm tests
fi
