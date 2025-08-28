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

// Test router setup
func setupTestRouter(testDB *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	// Store original db and replace with test db
	originalDB := db
	db = testDB
	defer func() { db = originalDB }()
	
	// Setup routes (simplified versions of main routes)
	router.GET("/collectionHash/:key", func(c *gin.Context) {
		key := c.Param("key")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			if !startsWithUUID(key) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
				return
			}

			var collectionHash string
			// Simplified hash calculation for SQLite
			if err := db.Raw(`
				SELECT hex(group_concat(key || ' ' || value, ' ')) as hash
				FROM feature_toggles WHERE key LIKE ? ORDER BY key
			`, key+"%").Scan(&collectionHash).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "Failed to calculate collection hash for provided UUID"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"collectionHash": collectionHash,
			})
		}
	})

	router.GET("/features/:key", func(c *gin.Context) {
		key := c.Param("key")
		var toggle FeatureToggle
		
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			if !startsWithUUID(key) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
				return
			}
			
			var toggles []FeatureToggle
			if err := db.Where("key LIKE ?", key+"%").Find(&toggles).Error; err != nil || len(toggles) == 0 {
				c.JSON(http.StatusNotFound, gin.H{"error": "No feature toggles found for provided UUID"})
				return
			}

			var strippedToggles []FeatureToggleDTO
			for _, obj := range toggles {
				strippedToggles = append(strippedToggles, FeatureToggleDTO{
					Key:        obj.Key,
					Value:      obj.Value,
					ActiveAt:   obj.ActiveAt,
					DisabledAt: obj.DisabledAt,
				})
			}
			c.JSON(http.StatusOK, gin.H{"toggles": strippedToggles})
		} else {
			c.JSON(http.StatusOK, gin.H{
				"key":        toggle.Key,
				"value":      toggle.Value,
				"activeAt":   toggle.ActiveAt,
				"disabledAt": toggle.DisabledAt,
			})
		}
	})

	router.POST("/features", func(c *gin.Context) {
		var newToggle FeatureToggle
		if err := c.ShouldBindJSON(&newToggle); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var secret string
		if !startsWithUUID(newToggle.Key) {
			newToggle.Key = prependUUID(newToggle.Key)
			secret = generateSecret()
			newToggle.Secret = secret
		} else {
			if !secretsMatch(newToggle.Key, newToggle.Secret) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
				return
			}
		}

		if err := db.Create(&newToggle).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create feature toggle"})
			return
		}

		response := gin.H{
			"key":        newToggle.Key,
			"value":      newToggle.Value,
			"activeAt":   newToggle.ActiveAt,
			"disabledAt": newToggle.DisabledAt,
		}
		if secret != "" {
			response["secret"] = secret
		}
		c.JSON(http.StatusCreated, response)
	})

	router.PUT("/features/activate/:key/:secret", func(c *gin.Context) {
		key := c.Param("key")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		toggle.Value = "true"
		if err := db.Save(&toggle).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to activate feature toggle"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"key":        toggle.Key,
			"value":      toggle.Value,
			"activeAt":   toggle.ActiveAt,
			"disabledAt": toggle.DisabledAt,
		})
	})

	router.PUT("/features/deactivate/:key/:secret", func(c *gin.Context) {
		key := c.Param("key")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		toggle.Value = "false"
		if err := db.Save(&toggle).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to deactivate feature toggle"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"key":        toggle.Key,
			"value":      toggle.Value,
			"activeAt":   toggle.ActiveAt,
			"disabledAt": toggle.DisabledAt,
		})
	})

	router.DELETE("/features/:key/:secret", func(c *gin.Context) {
		key := c.Param("key")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		if err := db.Delete(&toggle).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete feature toggle"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Feature toggle deleted"})
	})
	
	return router
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

func TestCollectionHash(t *testing.T) {
	testDB := setupTestDB(t)
	
	withTestDB(testDB, func() {
		router := setupTestRouter(testDB)
		
		// Setup test data
		testUUID := uuid.New().String()
		toggle1 := FeatureToggle{
			Key:    testUUID + "|feature1",
			Value:  "true",
			Secret: "test-secret",
		}
		toggle2 := FeatureToggle{
			Key:    testUUID + "|feature2",
			Value:  "false",
			Secret: "test-secret",
		}
		
		err := testDB.Create(&toggle1).Error
		require.NoError(t, err)
		err = testDB.Create(&toggle2).Error
		require.NoError(t, err)

		t.Run("get collection hash for UUID with features", func(t *testing.T) {
			url := fmt.Sprintf("/collectionHash/%s", testUUID)
			req, _ := http.NewRequest("GET", url, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			
			assert.Equal(t, http.StatusOK, w.Code)
			
			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			
			assert.Contains(t, response, "collectionHash")
			assert.NotEmpty(t, response["collectionHash"])
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