# Build gosu with latest Go to avoid CVEs in bundled gosu binary
FROM golang:1.25.7-alpine3.22 AS gosu-builder
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

# Replace bundled gosu with version built on latest Go (fixes stdlib CVEs)
COPY --from=gosu-builder /usr/local/bin/gosu /usr/local/bin/gosu

# Copy pg_cron extension from builder
COPY --from=builder /usr/local/lib/postgresql/pg_cron.so /usr/local/lib/postgresql/
COPY --from=builder /usr/local/share/postgresql/extension/pg_cron--*.sql /usr/local/share/postgresql/extension/
COPY --from=builder /usr/local/share/postgresql/extension/pg_cron.control /usr/local/share/postgresql/extension/

# Update shared_preload_libraries in default postgresql.conf
RUN echo "shared_preload_libraries = 'pg_cron'" >> /usr/local/share/postgresql/postgresql.conf.sample && \
    echo "cron.database_name = '${POSTGRES_DB:-postgres}'" >> /usr/local/share/postgresql/postgresql.conf.sample