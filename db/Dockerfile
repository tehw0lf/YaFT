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

# Update shared_preload_libraries in default postgresql.conf
RUN echo "shared_preload_libraries = 'pg_cron'" >> /usr/local/share/postgresql/postgresql.conf.sample && \
    echo "cron.database_name = '${POSTGRES_DB:-postgres}'" >> /usr/local/share/postgresql/postgresql.conf.sample

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
COPY db/init.sql /docker-entrypoint-initdb.d/init.sql
