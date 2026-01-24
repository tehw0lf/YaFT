# Build gosu with latest Go version to fix CVE-2025-58183 and CVE-2025-61729
FROM golang:1.25.6 AS gosu-builder
RUN git clone https://github.com/tianon/gosu.git /gosu && \
    cd /gosu && \
    git checkout e845c46b6bedb4457cda8c88814c30a53cc6c9e7 && \
    go mod download && \
    CGO_ENABLED=0 go build -v -ldflags '-d -s -w' -o /usr/local/bin/gosu

# PostgreSQL base image with security updates
FROM postgres:18-trixie
RUN apt-get update && \
  apt-get -y upgrade && \
  apt-get -y install postgresql-18-cron && \
  apt-get clean \
  && rm -rf /var/lib/apt/lists/*

# Replace gosu binary with version built using Go 1.25.6
COPY --from=gosu-builder /usr/local/bin/gosu /usr/local/bin/gosu
RUN chmod +x /usr/local/bin/gosu && \
    gosu --version