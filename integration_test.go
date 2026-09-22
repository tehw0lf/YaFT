//go:build integration

// Integration tests against a real PostgreSQL with pg_cron, initialised from
// db/init.sql. They cover what the SQLite suite structurally cannot: the
// PostgreSQL-only collectionHash query, the scheduled flips actually running
// under pg_cron, and the retention function.
//
// Run them with the stack in docker-compose-test.yml; they are skipped unless
// YAFT_TEST_DSN is set, so `go test ./...` stays fast and dependency-free.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// setupIntegrationDB connects to the container database and installs it as the
// global db for the duration of the test.
func setupIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("YAFT_TEST_DSN")
	if dsn == "" {
		t.Skip("YAFT_TEST_DSN not set; start docker-compose-test.yml to run integration tests")
	}

	testDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err, "connect to test database")

	// The schema comes from db/init.sql, which is what production runs.
	// AutoMigrate would mask a drift between the struct and that file, so it is
	// deliberately not called here.

	original := db
	db = testDB
	t.Cleanup(func() { db = original })

	return testDB
}

func integrationRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return setupRouter()
}

// TestIntegrationCollectionHash covers the handler's PostgreSQL-only query
// (digest/string_agg/array_to_string), which SQLite cannot execute.
func TestIntegrationCollectionHash(t *testing.T) {
	testDB := setupIntegrationDB(t)
	router := integrationRouter()

	testUUID := uuid.New().String()
	require.NoError(t, testDB.Create(&FeatureToggle{Key: testUUID + "|feature1", Value: "true", Secret: "s"}).Error)
	require.NoError(t, testDB.Create(&FeatureToggle{Key: testUUID + "|feature2", Value: "false", Secret: "s"}).Error)

	get := func() string {
		req, _ := http.NewRequest("GET", "/collectionHash/"+testUUID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		hash, ok := resp["collectionHash"].(string)
		require.True(t, ok, "collectionHash must be a string, got %v", resp["collectionHash"])
		return hash
	}

	first := get()
	assert.Len(t, first, 64, "sha256 hex digest")
	assert.Equal(t, first, get(), "hash must be stable while the group is unchanged")

	// Changing a value must change the hash, otherwise clients cannot use it to
	// detect updates.
	require.NoError(t, testDB.Model(&FeatureToggle{}).
		Where("key = ?", testUUID+"|feature2").
		Update("value", "true").Error)

	assert.NotEqual(t, first, get(), "hash must change when a toggle changes")
}

// TestIntegrationScheduledFlip proves the pg_cron jobs from db/init.sql flip
// toggles against now() rather than CURRENT_DATE.
//
// The old CURRENT_DATE comparison comes to midnight of the current day, so a
// timestamp earlier today did not satisfy active_at <= CURRENT_DATE and the
// flip was deferred until the next day. Scheduling one hour ago is therefore
// the discriminating case: it flips under now(), and would not under
// CURRENT_DATE.
//
// The jobs run once a minute, so this waits up to 90 seconds.
func TestIntegrationScheduledFlip(t *testing.T) {
	testDB := setupIntegrationDB(t)

	testUUID := uuid.New().String()
	anHourAgo := time.Now().Add(-time.Hour)

	activate := FeatureToggle{Key: testUUID + "|activate", Value: "false", ActiveAt: &anHourAgo, Secret: "s"}
	deactivate := FeatureToggle{Key: testUUID + "|deactivate", Value: "true", DisabledAt: &anHourAgo, Secret: "s"}
	require.NoError(t, testDB.Create(&activate).Error)
	require.NoError(t, testDB.Create(&deactivate).Error)

	// A toggle scheduled well into the future must not be touched, so a job that
	// simply rewrites every row cannot pass this test.
	future := time.Now().Add(24 * time.Hour)
	untouched := FeatureToggle{Key: testUUID + "|future", Value: "false", ActiveAt: &future, Secret: "s"}
	require.NoError(t, testDB.Create(&untouched).Error)

	valueOf := func(key string) string {
		var toggle FeatureToggle
		require.NoError(t, testDB.First(&toggle, "key = ?", key).Error)
		return toggle.Value
	}

	deadline := time.Now().Add(90 * time.Second)
	for {
		if valueOf(activate.Key) == "true" && valueOf(deactivate.Key) == "false" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pg_cron did not flip within 90s: activate=%q (want true), deactivate=%q (want false)",
				valueOf(activate.Key), valueOf(deactivate.Key))
		}
		time.Sleep(2 * time.Second)
	}

	assert.Equal(t, "false", valueOf(untouched.Key), "a future activeAt must not flip")
}

// TestIntegrationRetention covers cleanup_stale_feature_toggles: it removes a
// group only when every toggle in it is older than the window, and leaves
// groups with recent activity alone.
func TestIntegrationRetention(t *testing.T) {
	testDB := setupIntegrationDB(t)

	staleUUID := uuid.New().String()
	activeUUID := uuid.New().String()
	mixedUUID := uuid.New().String()

	create := func(key string) {
		require.NoError(t, testDB.Create(&FeatureToggle{Key: key, Value: "true", Secret: "s"}).Error)
	}
	// age backdates the GORM-maintained timestamps; they cannot be set on insert.
	age := func(key string, d time.Duration) {
		when := time.Now().Add(-d)
		require.NoError(t, testDB.Exec(
			`UPDATE feature_toggles SET created_at = ?, updated_at = ? WHERE key = ?`,
			when, when, key).Error)
	}

	create(staleUUID + "|a")
	create(staleUUID + "|b")
	age(staleUUID+"|a", 60*24*time.Hour)
	age(staleUUID+"|b", 45*24*time.Hour)

	create(activeUUID + "|a")

	// One old toggle, one recent one: the group is still in use and must survive
	// in full, including the old member.
	create(mixedUUID + "|old")
	create(mixedUUID + "|new")
	age(mixedUUID+"|old", 60*24*time.Hour)

	var deleted int64
	require.NoError(t, testDB.Raw(`SELECT cleanup_stale_feature_toggles()`).Scan(&deleted).Error)
	assert.EqualValues(t, 2, deleted, "only the two toggles of the stale group")

	count := func(prefix string) int64 {
		var n int64
		require.NoError(t, testDB.Model(&FeatureToggle{}).Where("key LIKE ?", prefix+"%").Count(&n).Error)
		return n
	}

	assert.EqualValues(t, 0, count(staleUUID), "stale group must be gone")
	assert.EqualValues(t, 1, count(activeUUID), "active group must survive")
	assert.EqualValues(t, 2, count(mixedUUID), "a group with recent activity keeps its old toggles")

	t.Run("never deletes rows with no timestamps", func(t *testing.T) {
		// Rows predating the created_at/updated_at columns have NULL in both.
		// COALESCE then yields NULL, the HAVING comparison is NULL, and the
		// group is excluded -- legacy rows survive until they are next written.
		// This is a data-loss guarantee, so it is asserted rather than assumed.
		legacyUUID := uuid.New().String()
		create(legacyUUID + "|legacy")
		require.NoError(t, testDB.Exec(
			`UPDATE feature_toggles SET created_at = NULL, updated_at = NULL WHERE key = ?`,
			legacyUUID+"|legacy").Error)

		var n int64
		require.NoError(t, testDB.Raw(`SELECT cleanup_stale_feature_toggles()`).Scan(&n).Error)
		assert.EqualValues(t, 0, n)
		assert.EqualValues(t, 1, count(legacyUUID), "rows without timestamps must survive")
	})

	t.Run("respects a custom retention window", func(t *testing.T) {
		shortUUID := uuid.New().String()
		create(shortUUID + "|a")
		age(shortUUID+"|a", 10*24*time.Hour)

		var n int64
		require.NoError(t, testDB.Raw(`SELECT cleanup_stale_feature_toggles(INTERVAL '5 days')`).Scan(&n).Error)
		assert.EqualValues(t, 1, n)
		assert.EqualValues(t, 0, count(shortUUID))
	})
}

// TestIntegrationRetentionNotScheduled guards the decision that the retention
// job stays off by default: a local stack must not silently delete data.
func TestIntegrationRetentionNotScheduled(t *testing.T) {
	testDB := setupIntegrationDB(t)

	var scheduled int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM cron.job WHERE command LIKE '%cleanup_stale_feature_toggles%'`).
		Scan(&scheduled).Error)

	assert.EqualValues(t, 0, scheduled,
		"retention must not be scheduled by default; operators opt in explicitly")
}

// TestIntegrationSchemaMatchesModel fails if the struct and db/init.sql drift
// apart. The integration database is built from init.sql without AutoMigrate,
// so a column added to the model but not to the file would otherwise only
// surface in production.
func TestIntegrationSchemaMatchesModel(t *testing.T) {
	testDB := setupIntegrationDB(t)

	for _, column := range []string{
		"id", "key", "value", "active_at", "disabled_at", "secret", "tags",
		"created_at", "updated_at",
	} {
		var exists bool
		require.NoError(t, testDB.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'feature_toggles' AND column_name = ?
			)`, column).Scan(&exists).Error)
		assert.Truef(t, exists, "db/init.sql is missing column %q required by the model", column)
	}

	// The model must round-trip through the shipped schema.
	key := uuid.New().String() + "|roundtrip"
	require.NoError(t, testDB.Create(&FeatureToggle{
		Key: key, Value: "true", Secret: "s", Tags: []string{"a", "b"},
	}).Error)

	var stored FeatureToggle
	require.NoError(t, testDB.First(&stored, "key = ?", key).Error)
	assert.Equal(t, []string{"a", "b"}, []string(stored.Tags))
	assert.False(t, stored.CreatedAt.IsZero(), "GORM must populate created_at")
	assert.False(t, stored.UpdatedAt.IsZero(), "GORM must populate updated_at")
}

// TestIntegrationOpenAPICollectionHash validates the 200 branch of
// /collectionHash against openapi.yaml.
//
// The SQLite suite can only reach the 404 branch, because the hash is built
// with PostgreSQL-only SQL. That leaves the success body -- the one a client
// actually parses -- unchecked against the spec unless it is done here.
func TestIntegrationOpenAPICollectionHash(t *testing.T) {
	testDB := setupIntegrationDB(t)
	router := integrationRouter()

	testUUID := uuid.New().String()
	require.NoError(t, testDB.Create(&FeatureToggle{
		Key: testUUID + "|feature1", Value: "true", Secret: "s",
	}).Error)

	req := httptest.NewRequest(http.MethodGet, specServer+"/collectionHash/"+testUUID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	validateAgainstSpec(t, req, rec)
}
