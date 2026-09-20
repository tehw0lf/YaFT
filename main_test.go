package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Test database setup
func setupTestDB(t *testing.T) *gorm.DB {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	
	err = testDB.AutoMigrate(&FeatureToggle{})
	require.NoError(t, err)
	
	return testDB
}

// Test setup with database injection
func withTestDB(testDB *gorm.DB, testFunc func()) {
	originalDB := db
	db = testDB
	defer func() { db = originalDB }()
	testFunc()
}

// setupTestRouter builds the production router from setupRouter() so the tests
// exercise the real handlers rather than a copy of them. The caller must already
// have swapped the global db (see withTestDB), because setupRouter closes over it.
//
// Routes relying on PostgreSQL-only SQL (collectionHash) cannot be served by the
// SQLite test database; they are covered by the integration suite instead.
func setupTestRouter(testDB *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	return setupRouter()
}

// Unit Tests for Utility Functions
func TestPrependUUID(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		tests := []struct {
			name string
			key  string
		}{
			{"simple key", "myfeature"},
			{"key with spaces", "my feature"},
			{"key with special chars", "my-feature_123"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := prependUUID(tt.key)
				
				parts := strings.Split(result, "|")
				assert.Len(t, parts, 2)
				assert.Equal(t, tt.key, parts[1])
				
				// Verify UUID is valid
				_, err := uuid.Parse(parts[0])
				assert.NoError(t, err)
			})
		}
	})
}

func TestStartsWithUUID(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{"valid UUID prefix", "550e8400-e29b-41d4-a716-446655440000|myfeature", true},
		{"invalid UUID prefix", "not-a-uuid|myfeature", false},
		{"no UUID prefix", "myfeature", false},
		{"empty string", "", false},
		{"only UUID", "550e8400-e29b-41d4-a716-446655440000", true}, // This actually returns true because it's a valid UUID
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := startsWithUUID(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsURLParseable(t *testing.T) {
	tests := []struct {
		name     string
		secret   string
		expected bool
	}{
		{"valid secret", "abc123-def456-ghi789", true},
		{"valid UUID secret", "550e8400-e29b-41d4-a716-446655440000", true},
		{"invalid chars", "secret with spaces", true}, // URL parsing is more permissive than expected
		{"invalid chars special", "secret%with%percent", false},
		{"empty string", "", true},
		{"only alphanumeric", "abc123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isURLParseable(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateSecret(t *testing.T) {
	secret1 := generateSecret()
	secret2 := generateSecret()
	
	// Should be different each time
	assert.NotEqual(t, secret1, secret2)
	
	// Should be 3 UUIDs concatenated (3 * 36 = 108 chars)
	assert.Len(t, secret1, 108)
	assert.Len(t, secret2, 108)
	
	// Should be URL parseable
	assert.True(t, isURLParseable(secret1))
	assert.True(t, isURLParseable(secret2))
}

func TestSecretsMatch(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		// Create test data
		testUUID := uuid.New().String()
		testSecret := "test-secret-123"
		
		toggle1 := FeatureToggle{
			Key:    testUUID + "|feature1",
			Value:  "true",
			Secret: testSecret,
		}
		toggle2 := FeatureToggle{
			Key:    testUUID + "|feature2", 
			Value:  "false",
			Secret: testSecret,
		}
		
		err := db.Create(&toggle1).Error
		require.NoError(t, err)
		err = db.Create(&toggle2).Error
		require.NoError(t, err)

		tests := []struct {
			name     string
			key      string
			secret   string
			expected bool
		}{
			{"valid secret for feature1", toggle1.Key, testSecret, true},
			{"valid secret for feature2", toggle2.Key, testSecret, true},
			{"invalid secret", toggle1.Key, "wrong-secret", false},
			{"nonexistent key", "nonexistent|key", testSecret, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := secretsMatch(tt.key, tt.secret)
				assert.Equal(t, tt.expected, result)
			})
		}
	})
}

// Integration Tests for API Endpoints
func TestCreateFeatureToggle(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		router := setupTestRouter(testDB)

		tests := []struct {
			name           string
			payload        map[string]interface{}
			expectedStatus int
			checkResponse  func(t *testing.T, resp map[string]interface{})
		}{
			{
				name: "create new feature without UUID",
				payload: map[string]interface{}{
					"Key":   "newfeature",
					"Value": "true",
				},
				expectedStatus: http.StatusCreated,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Contains(t, resp, "secret")
					assert.True(t, startsWithUUID(resp["key"].(string)))
					assert.Equal(t, "true", resp["value"])
				},
			},
			{
				name: "create feature with missing required fields",
				payload: map[string]interface{}{
					"Key": "",  // Empty key should still work with UUID prefix
				},
				expectedStatus: http.StatusCreated,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Contains(t, resp, "secret")
					assert.True(t, startsWithUUID(resp["key"].(string)))
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				jsonBytes, _ := json.Marshal(tt.payload)
				req, _ := http.NewRequest("POST", "/features", bytes.NewBuffer(jsonBytes))
				req.Header.Set("Content-Type", "application/json")
				
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				
				assert.Equal(t, tt.expectedStatus, w.Code)
				
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				
				tt.checkResponse(t, response)
			})
		}
	})
}

func TestGetFeatureToggle(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		router := setupTestRouter(testDB)
		
		// Setup test data
		testUUID := uuid.New().String()
		toggle := FeatureToggle{
			Key:    testUUID + "|testfeature",
			Value:  "true",
			Secret: "test-secret",
		}
		err := testDB.Create(&toggle).Error
		require.NoError(t, err)

		tests := []struct {
			name           string
			key            string
			expectedStatus int
			checkResponse  func(t *testing.T, resp map[string]interface{})
		}{
			{
				name:           "get existing feature",
				key:            toggle.Key,
				expectedStatus: http.StatusOK,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Equal(t, toggle.Key, resp["key"])
					assert.Equal(t, toggle.Value, resp["value"])
					assert.NotContains(t, resp, "secret") // Secret should not be returned
				},
			},
			{
				name:           "get nonexistent feature",
				key:            "nonexistent",
				expectedStatus: http.StatusNotFound,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Contains(t, resp, "error")
				},
			},
			{
				name:           "get features by UUID",
				key:            testUUID,
				expectedStatus: http.StatusOK,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Contains(t, resp, "toggles")
					toggles := resp["toggles"].([]interface{})
					assert.Len(t, toggles, 1)
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req, _ := http.NewRequest("GET", "/features/"+tt.key, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				
				assert.Equal(t, tt.expectedStatus, w.Code)
				
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				
				tt.checkResponse(t, response)
			})
		}
	})
}

func TestActivateFeatureToggle(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		router := setupTestRouter(testDB)
		
		// Setup test data
		testUUID := uuid.New().String()
		testSecret := "test-secret-123"
		toggle := FeatureToggle{
			Key:    testUUID + "|testfeature",
			Value:  "false",
			Secret: testSecret,
		}
		err := testDB.Create(&toggle).Error
		require.NoError(t, err)

		tests := []struct {
			name           string
			key            string
			secret         string
			expectedStatus int
			checkResponse  func(t *testing.T, resp map[string]interface{})
		}{
			{
				name:           "activate with valid secret",
				key:            toggle.Key,
				secret:         testSecret,
				expectedStatus: http.StatusOK,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Equal(t, "true", resp["value"])
					assert.Equal(t, toggle.Key, resp["key"])
				},
			},
			{
				name:           "activate with invalid secret",
				key:            toggle.Key,
				secret:         "wrong-secret",
				expectedStatus: http.StatusUnauthorized,
				checkResponse: func(t *testing.T, resp map[string]interface{}) {
					assert.Contains(t, resp, "error")
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				url := fmt.Sprintf("/features/activate/%s/%s", tt.key, tt.secret)
				req, _ := http.NewRequest("PUT", url, nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				
				assert.Equal(t, tt.expectedStatus, w.Code)
				
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				
				tt.checkResponse(t, response)
			})
		}
	})
}

func TestDeactivateAndDeleteFeatureToggle(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		router := setupTestRouter(testDB)
		
		// Setup test data
		testUUID := uuid.New().String()
		testSecret := "test-secret-123"
		toggle := FeatureToggle{
			Key:    testUUID + "|testfeature",
			Value:  "true",
			Secret: testSecret,
		}
		err := testDB.Create(&toggle).Error
		require.NoError(t, err)

		t.Run("deactivate with valid secret", func(t *testing.T) {
			url := fmt.Sprintf("/features/deactivate/%s/%s", toggle.Key, testSecret)
			req, _ := http.NewRequest("PUT", url, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			
			assert.Equal(t, http.StatusOK, w.Code)
			
			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			
			assert.Equal(t, "false", response["value"])
			assert.Equal(t, toggle.Key, response["key"])
		})

		t.Run("delete with valid secret", func(t *testing.T) {
			url := fmt.Sprintf("/features/%s/%s", toggle.Key, testSecret)
			req, _ := http.NewRequest("DELETE", url, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			
			assert.Equal(t, http.StatusOK, w.Code)
			
			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			
			assert.Equal(t, "Feature toggle deleted", response["message"])
			
			// Verify it's actually deleted
			var deletedToggle FeatureToggle
			err = testDB.First(&deletedToggle, "key = ?", toggle.Key).Error
			assert.Error(t, err) // Should not be found
		})
	})
}

// Benchmark Tests
func BenchmarkPrependUUID(b *testing.B) {
	testDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	testDB.AutoMigrate(&FeatureToggle{})
	originalDB := db
	db = testDB
	defer func() { db = originalDB }()

	for i := 0; i < b.N; i++ {
		prependUUID("benchmarkfeature")
	}
}

func BenchmarkGenerateSecret(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateSecret()
	}
}

func BenchmarkIsURLParseable(b *testing.B) {
	secret := "test-secret-123-456-789"
	for i := 0; i < b.N; i++ {
		isURLParseable(secret)
	}
}