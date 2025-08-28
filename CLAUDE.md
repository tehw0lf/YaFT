# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

YaFT (Yet another Feature Toggle) is a simple REST API service built in Go that provides feature toggle management with UUID-based grouping, time-based activation/deactivation, and secure secret-based authentication. The service uses PostgreSQL with GORM as the database layer and includes automated scheduling via pg_cron.

## Architecture

### Core Components
- **Single Go File Architecture**: All API logic resides in `main.go` (652 lines)
- **Database**: PostgreSQL with GORM ORM and automated migrations
- **Web Framework**: Gin HTTP router for REST API endpoints
- **Authentication**: Secret-based authentication for write operations
- **Logging**: Structured logging with Logrus (JSON format)
- **Containerization**: Docker multi-stage builds with distroless runtime

### Data Model
```go
type FeatureToggle struct {
    ID         uint       `gorm:"primaryKey"`
    Key        string     `gorm:"unique;not null"`  // Format: UUID|feature_name
    Value      string     `gorm:"not null"`         // "true" or "false"
    ActiveAt   *time.Time `gorm:"null"`
    DisabledAt *time.Time `gorm:"null"`
    Secret     string     `gorm:"null"`
}
```

### Key Architecture Patterns
- **UUID-based Grouping**: Feature toggles are grouped by prepending UUIDs (e.g., `896ea308-382f-46b0-bc59-d93a28013633|myFeature`)
- **Secret Management**: Shared secrets across feature toggle groups, generated only for the first toggle in a group
- **Time-based Scheduling**: PostgreSQL cron jobs automatically activate/deactivate features based on `ActiveAt`/`DisabledAt` timestamps
- **DTO Pattern**: `FeatureToggleDTO` excludes secrets from public API responses

## Development Commands (Docker-based)

### Local Development
```bash
# Start development environment with local builds
docker compose -f docker-compose-local.yml up --force-recreate --build

# View logs
docker compose -f docker-compose-local.yml logs -f

# Stop and clean up
docker compose -f docker-compose-local.yml down --volumes
```

### Testing and Quality (Docker-based)
```bash
# Run all tests with verbose output
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v ./...

# Run tests with coverage report
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -cover ./...

# Run tests with detailed coverage profile
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -coverprofile=coverage.out ./...

# Generate HTML coverage report
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go tool cover -html=coverage.out -o coverage.html

# Run specific test functions
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -run TestCreateFeatureToggle ./...

# Run benchmark tests
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -bench=. ./...

# Run tests with race detection
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -race ./...

# Format code using Docker
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go fmt ./...

# Check and download modules
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go mod tidy
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go mod download

# Build binary in Docker
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go build -o yaft main.go

# Vet code for common issues
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go vet ./...
```

### Database Operations (Docker-based)
```bash
# Access PostgreSQL directly (when running via docker-compose)
docker compose -f docker-compose-local.yml exec db psql -U postgres -d yaft

# Check cron jobs in database
docker compose -f docker-compose-local.yml exec db psql -U postgres -d yaft -c "SELECT * FROM cron.job;"

# Manual feature toggle queries
docker compose -f docker-compose-local.yml exec db psql -U postgres -d yaft -c "SELECT * FROM feature_toggles WHERE key LIKE 'uuid%';"
```

## Docker Usage

### Production Deployment
```bash
# Using pre-built images from GitHub Container Registry
docker compose up

# Environment variables required in .env:
# POSTGRES_USER=postgres
# POSTGRES_PASSWORD=your_secure_password
# POSTGRES_DB=yaft
```

### Local Development
```bash
# Build and run with local source
docker compose -f docker-compose-local.yml up --build

# Rebuild specific service
docker compose -f docker-compose-local.yml up --build app
```

## API Endpoints Architecture

### Public Endpoints (No Authentication)
- `GET /features/{key}` - Retrieve specific feature or all features for UUID group
- `GET /collectionHash/{uuid}` - Get SHA256 hash of all features in a group

### Protected Endpoints (Require Secret)
- `POST /features` - Create new feature toggle
- `PUT /features/activate/{key}/{secret}` - Activate feature
- `PUT /features/activateAt/{key}/{date}/{secret}` - Schedule activation
- `PUT /features/deactivate/{key}/{secret}` - Deactivate feature  
- `PUT /features/deactivateAt/{key}/{date}/{secret}` - Schedule deactivation
- `DELETE /features/{key}/{secret}` - Delete feature toggle
- `PUT /secret/update/{uuid}/{oldsecret}/{newsecret}` - Update group secret

### Key Functions
- **`prependUUID(key)`**: Generates unique UUID prefix for new feature groups
- **`secretsMatch(key, secret)`**: Validates secrets against feature toggle groups
- **`startsWithUUID(key)`**: Checks if key already has UUID prefix
- **`generateSecret()`**: Creates 3x UUID concatenated secret
- **`isURLParseable(secret)`**: Validates secrets can be used in URLs

## Database Schema and Automation

### PostgreSQL Extensions
- `pg_cron`: Automated scheduling for time-based feature activation
- `pgcrypto`: Cryptographic functions for collection hashing

### Automated Jobs
Two cron jobs run every minute to handle scheduled feature toggles:
```sql
-- Activate features when active_at <= current date
UPDATE feature_toggles SET value = 'true' WHERE active_at <= CURRENT_DATE;

-- Deactivate features when disabled_at <= current date  
UPDATE feature_toggles SET value = 'false' WHERE disabled_at <= CURRENT_DATE;
```

## Environment Configuration

### Required Environment Variables
- `DB_DSN`: PostgreSQL connection string (format: `postgres://user:password@host:port/database`)
- `POSTGRES_USER`: Database username
- `POSTGRES_PASSWORD`: Database password
- `POSTGRES_DB`: Database name

### Database Connection Handling
The application includes retry logic (10 attempts with 5-second delays) for PostgreSQL connection establishment to handle container startup dependencies.

## Security Considerations

### Secret Management
- Secrets are only returned when creating the first feature in a group
- Secrets are excluded from all public API responses via DTO pattern
- URL validation ensures secrets are URL-safe for REST endpoints
- Secrets are shared across all features in a UUID group

### Input Validation
- UUID validation for feature grouping
- Time parsing validation for scheduled operations
- Secret matching validation for all write operations
- GORM handles SQL injection prevention

## Development Workflow

### Adding New Endpoints
1. Define handler function following existing pattern with structured logging
2. Add route registration in `main()` function
3. Include proper secret validation for write operations
4. Use `FeatureToggleDTO` for responses that should exclude secrets
5. Add comprehensive error logging with context fields

### Database Changes
- Schema changes require updating `db/init.sql`
- GORM handles automatic migrations for struct changes
- Cron job modifications require database recreation

### Code Architecture Changes (For Testing)
The codebase has been refactored to improve testability:
- **Database Initialization**: Separated into `initDatabase()` and `setupDatabase()` functions
- **Testable Design**: Global `db` variable can be replaced with test database
- **No Init Dependencies**: The `init()` function only configures logging, not database connections
- **Dependency Injection**: Database connection is established in `main()` via `setupDatabase()`

### Testing Features
- **Unit Tests**: Test individual utility functions with in-memory SQLite
- **Integration Tests**: Full HTTP endpoint testing with test routers
- **Benchmark Tests**: Performance testing for critical functions
- Use comprehensive curl examples in README.md for manual API testing (service runs on port 8080)

## Test Architecture

### Test Structure
The test suite is organized in `main_test.go` and includes:

#### Unit Tests
- **Utility Function Tests**: Test core functions like `prependUUID`, `startsWithUUID`, `isURLParseable`, `generateSecret`, and `secretsMatch`
- **Database Helper Tests**: Test database connection and setup functions
- **Validation Logic Tests**: Test input validation and security checks

#### Integration Tests  
- **API Endpoint Tests**: Full HTTP request/response testing for all endpoints
- **Database Integration Tests**: Test database operations with real GORM queries
- **Authentication Flow Tests**: Test secret-based authentication across all protected endpoints
- **Error Handling Tests**: Test error responses and edge cases

#### Test Database
- Uses **SQLite in-memory database** for isolated, fast testing
- Automatic schema migration using GORM AutoMigrate
- Clean database state for each test function
- No external dependencies required for testing

### Key Testing Patterns

#### Test Setup Functions
```go
// setupTestDB creates isolated in-memory SQLite database
func setupTestDB(t *testing.T) *gorm.DB

// setupTestRouter creates Gin router with test database
func setupTestRouter(testDB *gorm.DB) *gin.Engine
```

#### Test Categories
1. **Unit Tests**: `TestPrependUUID`, `TestStartsWithUUID`, `TestIsURLParseable`, `TestGenerateSecret`, `TestSecretsMatch`
2. **API Integration**: `TestCreateFeatureToggle`, `TestGetFeatureToggle`, `TestActivateFeatureToggle`, `TestDeactivateFeatureToggle`, `TestDeleteFeatureToggle`, `TestCollectionHash`  
3. **Performance**: `BenchmarkPrependUUID`, `BenchmarkGenerateSecret`, `BenchmarkIsURLParseable`

### Test Data Management
- UUID-based test data with predictable secrets
- Automatic cleanup between tests
- Factory functions for creating test fixtures
- No persistent test data dependencies

### Running Specific Test Types
```bash
# Run only unit tests (utility functions)
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -run "Test.*UUID|TestGenerateSecret|TestIsURLParseable" ./...

# Run only integration tests (API endpoints) 
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -run "Test.*Feature|TestCollection" ./...

# Run only benchmark tests
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 go test -v -run ^$ -bench . ./...
```

## CI/CD Integration

### GitHub Actions Workflow
The project uses a reusable workflow from `tehw0lf/workflows` that automatically runs tests during the Docker build process. The Dockerfile includes a dedicated test stage:

```dockerfile
# Run the tests in the container
FROM build-stage AS run-test-stage
RUN go mod tidy && go vet ./... && go test -race -cover ./...
```

This ensures that during CI/CD:
- Dependencies are properly tidied
- Code passes static analysis (`go vet`)  
- All tests pass with race detection and coverage reporting
- Docker build fails if any tests fail
- Only successful builds are published to the container registry

### Pre-commit Validation Commands (Docker-based)

```bash
# Full validation pipeline (matches CI/CD)
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 bash -c "go mod tidy && go vet ./... && go test -race -cover ./... && go build"

# Quick validation (for faster feedback)
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 bash -c "go mod tidy && go test ./... && go build"

# Coverage-focused validation
docker run --rm -v $(pwd):/app -w /app golang:1.24.1 bash -c "go mod tidy && go test -cover ./... && go build"
```