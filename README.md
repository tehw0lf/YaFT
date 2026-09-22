# YaFT - Yet another Feature Toggle

<div align="center">
  <img src="./logo.svg" alt="YaFT Logo" width="140">
</div>

---

Provides a simple feature toggle API that supports strings as keys and stringified booleans (`"true"` or `"false"`) as values.
Features can be given an optional start and end date.
Features are grouped by prepending UUIDs. This is done automatically on creating the first feature in a group. To add more features to the group, prepend the new feature's key with a UUIDv4 followed by a pipe symbol `|` (see examples)
All endpoints apart from the `GET` endpoint require a secret. This is created when creating a new feature toggle without a UUIDv4 and returned from the `POST` request. The secret is only returned upon creating the _first_ feature toggle in a group.

---

# Usage:
## Prod usage (EARLY ALPHA)
Please note that this program is in an alpha state and has not been tested. Use at your own risk!
Copy and adapt `docker-compose.yml` or build your own setup.

## Local usage (for development)
`docker compose -f docker-compose-local.yml up --force-recreate --build`

## Tests

The fast suite runs against an in-memory SQLite database and needs nothing else:

`docker run --rm -v $(pwd):/app -w /app golang:1.27.0 go test -race -cover ./...`

The integration suite runs against a real PostgreSQL with pg_cron, initialised
from `db/init.sql`. It covers what SQLite structurally cannot: the
`collectionHash` query, the scheduled flips actually firing under pg_cron, and
the retention function. It takes about a minute, because it waits for a real
cron tick:

```bash
scripts/integration-tests.sh
```

The script generates a throwaway database password for the run, waits for
PostgreSQL and tears the stack down afterwards. No credential is stored in the
repository.

To drive the stack by hand, export a password first and keep `--env-file
/dev/null` on every call -- otherwise Compose resolves `POSTGRES_PASSWORD` from
the `.env` used for local development:

```bash
export POSTGRES_PASSWORD="$(openssl rand -hex 16)"
docker compose --env-file /dev/null -f docker-compose-test.yml up -d --build testdb
docker compose --env-file /dev/null -f docker-compose-test.yml run --rm tests
docker compose --env-file /dev/null -f docker-compose-test.yml down --volumes
```

## Data retention

`POST /features` is unauthenticated for the first toggle of a group -- it has
to be, because that call is what issues the secret -- so on a public instance
anyone can create rows.

`db/init.sql` therefore ships a `cleanup_stale_feature_toggles()` function that
deletes a whole toggle group once none of its members has been modified within
the retention window (30 days by default). Grouping is by the UUID prefix, so
a group still in use never loses individual toggles.

**It is not scheduled by default**, so a local stack never deletes your data.
Enable it on a public instance:

```sql
SELECT cron.schedule('0 3 * * *', $$ SELECT cleanup_stale_feature_toggles(); $$);
```

Pass a different window if 30 days does not fit:

```sql
SELECT cleanup_stale_feature_toggles(INTERVAL '90 days');
```

The image ships `init.sql`, so a deployment that only pulls
`ghcr.io/tehw0lf/yaft-db` gets the schema, the pg_cron extension and the
scheduled flips without needing a checkout of this repository. The local and
test compose files still bind mount the file so an edit takes effect without a
rebuild.

Worth knowing, because the failure is quiet: GORM's AutoMigrate creates the
table on first connect, so a database started without `init.sql` looks healthy
and serves requests. Only the time-based flipping is missing.

## Upgrading an existing database

`db/init.sql` only runs when the data directory is empty, so an existing
deployment needs these applied by hand.

The scheduled flips changed from `CURRENT_DATE` to `now()` (see
[When the flip happens](#when-the-flip-happens)). Replace the old jobs:

```sql
-- inspect first: note the jobids of the two feature_toggles jobs
SELECT jobid, command FROM cron.job;
SELECT cron.unschedule(<jobid>);  -- for each of the two
```

Then re-run the two `cron.schedule` statements and the
`CREATE OR REPLACE FUNCTION` block from `db/init.sql`.

The `created_at` and `updated_at` columns the retention function needs are
added automatically by GORM's AutoMigrate on startup. On existing rows they
start out `NULL`; the function falls back to `created_at` and then ignores such
rows until they are next written, so nothing is deleted on the basis of a
missing timestamp.

# Releasing

Images are published to `ghcr.io/tehw0lf/yaft` and `ghcr.io/tehw0lf/yaft-db`.

The project version lives in the `VERSION` file. Go has no version field in its
manifest -- `go 1.27.0` in `go.mod` is the language version -- so the release
tooling reads `VERSION` instead, the same way prometheus and consul do it.

Bump it in the PR that should be released:

```bash
echo 0.2.0 > VERSION
```

On the next push to `main` the pipeline tags the commit `v0.2.0`, pushes that
tag, and publishes `ghcr.io/tehw0lf/yaft:v0.2.0` and `yaft-db:v0.2.0` alongside
`:latest`. An existing version tag is skipped rather than overwritten, so
forgetting to bump republishes `:latest` only.

GHCR tags stay mutable regardless, so pin the image digest
(`ghcr.io/tehw0lf/yaft@sha256:...`) when a deployment has to be reproducible.

`:latest` is overwritten on every push to `main`, which also means base image
security patches only reach `:latest` when something is pushed.

# API Interaction

The full contract -- every path, parameter, status code and response schema --
is in [`openapi.yaml`](openapi.yaml). It is validated against the real handlers
by `openapi_test.go`, so unlike the examples below it cannot quietly fall
behind. Generate a client from it rather than hand-writing one:

```bash
# Go -- yields a typed method per endpoint, *time.Time for the nullable
# dates and *[]string for tags, so the null-versus-[] trap is handled
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest \
  -package yaft -generate types,client openapi.yaml > client.go

# TypeScript types
npx openapi-typescript openapi.yaml -o yaft.d.ts

# Java, Python, C#, ...
npx @openapitools/openapi-generator-cli generate -i openapi.yaml -g java
```

A generated client covers the transport only. When a feature counts as enabled
-- the boundary behaviour of `activeAt`/`disabledAt`, which timestamp formats
are ignored -- is decided by the client itself and specified in
[yaft-conformance](https://github.com/tehw0lf/yaft-conformance), because
scheduled flips reach the stored `value` up to a minute late.

The examples below are the same calls by hand.

## Creating new Feature Toggles

`curl -d '{"Key":"myKey","Value":"true"}' -X POST "http://127.0.0.1:8080/features"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true","activeAt":null,"disabledAt":null,"tags":null,"secret":"example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"}`

error response:
`{"error":"Failed to create feature toggle"}`

### Validation

`Value` must be exactly `"true"` or `"false"`. Anything else -- `"TRUE"`,
`"1"`, an empty value or a missing one -- is rejected:

`{"error":"Invalid value, expected \"true\" or \"false\""}`

This matters because a client library reads any value other than `"true"` as
"off". Storing `"TRUE"` would look enabled in the database and be disabled in
every consumer.

`Key` is limited to 256 characters including the generated UUID prefix, so a
single request cannot create an unbounded row. Keys sent without a prefix are
checked against the remaining budget, since the prefix is added server-side:

`{"error":"Key too long, maximum is 219 characters"}`

Existing rows are not affected by either rule.

## Creating new Feature Toggles with existing UUID

`curl -d '{"Key":"896ea308-382f-46b0-bc59-d93a28013633|myOtherKey","Value":"true","Secret":"example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"}' -X POST "http://127.0.0.1:8080/features"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633|myOtherKey","value":"true","activeAt":null,"disabledAt":null,"tags":null}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but key exists:
`{"error":"Failed to create feature toggle"}`

## Deleting a specific Feature Toggle

`curl -X DELETE "http://127.0.0.1:8080/features/896ea308-382f-46b0-bc59-d93a28013633|myKey/example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"`

### Responses

successful response:
`{"message":"Feature toggle deleted"}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Feature not found"}`

## Activate a Feature Toggle

`curl -X PUT "http://127.0.0.1:8080/features/activate/896ea308-382f-46b0-bc59-d93a28013633|myKey/example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true","activeAt":null,"disabledAt":null,"tags":null}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to activate feature toggle"}`

## Activate a Feature Toggle at a certain date

`curl -X PUT "http://127.0.0.1:8080/features/activateAt/896ea308-382f-46b0-bc59-d93a28013633|myKey/2026-10-10T15:00:00Z/example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true","activeAt":"2026-10-10T15:00:00Z","disabledAt":null,"tags":null}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to activate feature toggle at"}`

error response if the date is not RFC 3339 with an offset:
`{"error":"Invalid date, expected RFC 3339 with offset"}`

### Date format

Dates must be RFC 3339 **with an offset** (`2026-10-10T15:00:00Z` or
`2026-10-10T15:00:00+02:00`). A bare date such as `2026-10-10` is rejected,
because languages disagree on how to interpret one -- JavaScript reads it as
UTC midnight, others as local midnight -- and a toggle would flip at a
different moment depending on the client.

### When the flip happens

Scheduled toggles are flipped by a job inside PostgreSQL that runs **once a
minute**, so the value changes within 60 seconds of the scheduled time, not
exactly at it.

A client library that evaluates `activeAt`/`disabledAt` itself is therefore
briefly ahead of the backend. That is intended: both compare against the wall
clock, so they agree on the intended moment and differ only by the polling
delay.

## Deactivate a Feature Toggle

`curl -X PUT "http://127.0.0.1:8080/features/deactivate/896ea308-382f-46b0-bc59-d93a28013633|myKey/example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"`

### Responses

successful response:
`{"key":"88ce4805-92a5-4774-ac05-5ebf12de9a58|a","value":"false","activeAt":null,"disabledAt":null,"tags":null}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to deactivate feature toggle"}`

## Deactivate a Feature Toggle at a certain date

`curl -X PUT "http://127.0.0.1:8080/features/deactivateAt/896ea308-382f-46b0-bc59-d93a28013633|myKey/2026-10-10T15:00:00Z/example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff"`

### Responses

successful response:
`{"key":"88ce4805-92a5-4774-ac05-5ebf12de9a58|a","value":"false","activeAt":null,"disabledAt":"2026-10-10T15:00:00Z","tags":null}`

error response if secret is wrong:
`{"error":"Invalid secret"}`

error response if secret is correct but feature was not found:
`{"error":"Failed to deactivate feature toggle"}`

## Getting a specific Feature Toggle

`curl "http://127.0.0.1:8080/features/896ea308-382f-46b0-bc59-d93a28013633|myKey"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true","activeAt":null,"disabledAt":null,"tags":null}`

error response:
`{"error":"Feature not found"}`

## Getting all Feature Toggles for a given UUID

`curl "http://127.0.0.1:8080/features/896ea308-382f-46b0-bc59-d93a28013633"`

### Responses

successful response:
`{"toggles":[{"key":"896ea308-382f-46b0-bc59-d93a28013633|myKey","value":"true","activeAt":null,"disabledAt":null,"tags":null},{"key":"896ea308-382f-46b0-bc59-d93a28013633|myOtherKey","value":"true","activeAt":null,"disabledAt":null,"tags":null}]}`

The field names are the same here as for a single toggle. Up to 0.1.6 this
response spelled them `Key`, `Value`, `ActiveAt` and `DisabledAt`, because the
response DTO carried no JSON tags while the single-toggle response was written
out by hand. A client that has to work with older instances should accept both
spellings; see R22a in
[yaft-conformance](https://github.com/tehw0lf/yaft-conformance).

An unset `tags` is `null`, not `[]`.

## Getting the collection hash for a given UUID

`curl "http://127.0.0.1:8080/collectionHash/896ea308-382f-46b0-bc59-d93a28013633"`

### Responses

successful response:
`{"collectionHash":"dce01876b3f0c843fb2c1e5efe54bf807dc991eefc660d112306b49f6e2335c6"}`

error response:
`{"error":"Feature not found"}`

## Updating a secret for a given UUID

`curl -X PUT "http://127.0.0.1:8080/secret/update/896ea308-382f-46b0-bc59-d93a28013633/example0-0000-4000-8000-0000000000ffexample1-0000-4000-8000-0000000000ffexample2-0000-4000-8000-0000000000ff/mynewsecret"`

### Responses

successful response:
`{"key":"896ea308-382f-46b0-bc59-d93a28013633"}`

error response if secret is invalid or UUID does not exist:
`{"error":"Invalid secret"}`

error response if new secret is not URL parseable:
`{"error": "New secret is not URL parseable, aborting operation"}`

# Licenses

- Code: MIT License
- Logo/Branding: All rights reserved