# syntax=docker/dockerfile:1

# Build the application from source
FROM golang:1.27.4-alpine AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /yaft

# Run the tests in the container (a local check, not built by the CI publish step).
# cgo is needed for -race and for the go-sqlite3 test driver; the data files are
# read by the OpenAPI tests.
FROM build-stage AS run-test-stage
COPY openapi.yaml VERSION ./
RUN apk add --no-cache gcc musl-dev
ENV CGO_ENABLED=1
RUN go mod tidy && go vet ./... && go test -race -cover ./...

# Deploy the application binary into a lean Alpine image
FROM alpine:3.22 AS build-release-stage

# Upgrade all packages to get latest security patches, then install ca-certificates
RUN apk --no-cache upgrade && apk --no-cache add ca-certificates

WORKDIR /

# Create non-root user
RUN addgroup -g 65532 -S nonroot && \
    adduser -u 65532 -S nonroot -G nonroot

COPY --from=build-stage /yaft /yaft

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/yaft"]