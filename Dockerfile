# syntax=docker/dockerfile:1

# Build the application from source
FROM golang:1.25.6-alpine AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /yaft

# Run the tests in the container
FROM build-stage AS run-test-stage
RUN go mod tidy && go vet ./... && go test -race -cover ./...

# Deploy the application binary into a lean Alpine image
FROM alpine:3.21 AS build-release-stage

# Install ca-certificates for HTTPS connections
RUN apk --no-cache add ca-certificates

WORKDIR /

# Create non-root user
RUN addgroup -g 65532 -S nonroot && \
    adduser -u 65532 -S nonroot -G nonroot

COPY --from=build-stage /yaft /yaft

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/yaft"]