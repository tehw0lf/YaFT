# Build gosu with latest Go to avoid CVEs in bundled gosu binary
FROM golang:1.27.0-alpine3.23 AS gosu-builder
RUN apk --no-cache add git && \
    git clone --depth=1 https://github.com/tianon/gosu.git /gosu && \
    cd /gosu && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/gosu . && \
    chmod +x /usr/local/bin/gosu

# PostgreSQL Alpine base image
FROM postgres:18-alpine3.22 AS builder

# Install build dependencies for pg_cron
RUN apk --no-cache add \
    postgresql-dev \
    build-base \
    clang19 \
    llvm19-dev \
    git \
    make

# Build pg_cron extension
WORKDIR /tmp
RUN git clone https://github.com/citusdata/pg_cron.git && \
    cd pg_cron && \
    make && \
    make install

# Final PostgreSQL Alpine image with pg_cron
FROM postgres:18-alpine3.22

# Upgrade all packages to get latest security patches (fixes OpenSSL/musl CVEs)
RUN apk --no-cache upgrade

# Replace bundled gosu with version built on latest Go (fixes stdlib CVEs)
COPY --from=gosu-builder /usr/local/bin/gosu /usr/local/bin/gosu

# Copy pg_cron extension from builder
COPY --from=builder /usr/local/lib/postgresql/pg_cron.so /usr/local/lib/postgresql/
COPY --from=builder /usr/local/share/postgresql/extension/pg_cron--*.sql /usr/local/share/postgresql/extension/
COPY --from=builder /usr/local/share/postgresql/extension/pg_cron.control /usr/local/share/postgresql/extension/

# Update shared_preload_libraries in default postgresql.conf.
#
# cron.database_name is deliberately absent here and supplied by the entrypoint
# as a server argument instead. It cannot be settled at build time:
# ${POSTGRES_DB} would expand to whatever the build environment had --
# 'postgres' in practice -- and pg_cron refuses CREATE EXTENSION from any other
# database, so the container would exit during init rather than merely losing
# its scheduled flips.
RUN echo "shared_preload_libraries = 'pg_cron'" >> /usr/local/share/postgresql/postgresql.conf.sample

# Ship the schema with the image.
#
# Without this the schema only exists where db/init.sql happens to be bind
# mounted, which is to say: on a checkout of this repository. A deployment
# pulling ghcr.io/tehw0lf/yaft-db would come up with no pg_cron extension, no
# table and no scheduled flips -- and would look healthy while doing it,
# because GORM's AutoMigrate creates the table on first connect. Only the
# time-based flipping would be silently missing.
#
# Postgres runs this once, when the data directory is empty. An existing
# deployment still needs the manual steps in the README.
# --chmod=0644 explicitly: COPY preserves the build host's permissions, so a
# restrictive umask produces a file postgres cannot read, and the container
# exits with "psql: error: ...init.sql: Permission denied". The entrypoint
# script next to it already sets its mode for the same reason.
COPY --chmod=0644 db/init.sql /docker-entrypoint-initdb.d/init.sql

# 0755 explicitly: COPY preserves the build host's permissions, and a
# restrictive umask there produced 0751, which is unreadable for the postgres
# user the stock entrypoint switches to.
COPY --chmod=0755 db/entrypoint.sh /usr/local/bin/yaft-entrypoint.sh

# No USER directive, deliberately, and this is the upstream postgres image's
# design rather than an oversight. The entrypoint has to start as root to
# initialise the data directory and fix its ownership; it then hands the
# server over with `exec gosu postgres`, so the long-running process does not
# run as root. Adding USER postgres here breaks initialisation instead of
# hardening anything -- verified: the container exits during startup, since
# the configuration file it must rewrite is root-owned.
#
# The suppressions have to sit directly above the flagged lines, so the
# reasoning is above rather than between them.
# nosemgrep: dockerfile.security.missing-user-entrypoint.missing-user-entrypoint
ENTRYPOINT ["yaft-entrypoint.sh"]
# nosemgrep: dockerfile.security.missing-user.missing-user
CMD ["postgres"]
