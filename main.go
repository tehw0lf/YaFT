package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// MaxKeyLength caps the stored key, including the generated UUID prefix, so a
// single request cannot create an unbounded row.
const MaxKeyLength = 256

// keySeparator divides the UUID prefix from the caller-supplied feature name.
const keySeparator = "|"

type FeatureToggle struct {
	ID         uint           `gorm:"primaryKey"`
	Key        string         `gorm:"unique;not null"`
	Value      string         `gorm:"not null"`
	ActiveAt   *time.Time     `gorm:"null"`
	DisabledAt *time.Time     `gorm:"null"`
	Secret     string         `gorm:"null"`
	Tags       pq.StringArray `gorm:"type:text[]"`
	// Maintained by GORM. The retention job groups on UpdatedAt to decide
	// whether a feature toggle group is stale.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FeatureToggleDTO is what leaves the API: a FeatureToggle without the ID and
// without the secret.
//
// The JSON tags are not cosmetic. Without them Go marshals the field names as
// written, so a UUID group came back capitalised (`Key`, `Value`) while a
// single toggle -- written by hand as a gin.H literal -- came back lowercase.
// Two spellings for one resource meant every client had to normalise both, and
// the usual `a.Key || a.key` idiom silently drops a legitimately empty value.
type FeatureToggleDTO struct {
	Key        string         `json:"key"`
	Value      string         `json:"value"`
	ActiveAt   *time.Time     `json:"activeAt"`
	DisabledAt *time.Time     `json:"disabledAt"`
	Tags       pq.StringArray `json:"tags"`
}

// toDTO strips a FeatureToggle down to what callers may see. Every handler
// that returns a toggle goes through it, so there is one definition of the
// response shape rather than one per handler.
func toDTO(toggle FeatureToggle) FeatureToggleDTO {
	return FeatureToggleDTO{
		Key:        toggle.Key,
		Value:      toggle.Value,
		ActiveAt:   toggle.ActiveAt,
		DisabledAt: toggle.DisabledAt,
		Tags:       toggle.Tags,
	}
}

var db *gorm.DB
var logger = logrus.New()

func initDatabase(dsn string) (*gorm.DB, error) {
	var database *gorm.DB
	var err error

	// Wait for PostgreSQL to be available
	for i := 0; i < 10; i++ {
		database, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err == nil {
			break
		}
		logger.Warn("Failed to connect database:", err)
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		return nil, err
	}

	// Auto-migrate the schema
	err = database.AutoMigrate(&FeatureToggle{})
	if err != nil {
		return nil, err
	}

	return database, nil
}

func prependUUID(key string) string {
	newUUID := uuid.New().String()
	for db.Where("key LIKE ?", newUUID+"%").Error != nil {
		newUUID = uuid.New().String()
	}
	return newUUID + keySeparator + key
}

func startsWithUUID(key string) bool {
	firstPart := strings.Split(key, keySeparator)[0]
	_, err := uuid.Parse(firstPart)
	return err == nil
}

func isURLParseable(secret string) bool {
	_, err := url.ParseRequestURI("https://example.com/" + secret)
	return err == nil
}

func generateSecret() string {
	return uuid.New().String() + uuid.New().String() + uuid.New().String()
}

func secretsMatch(key string, secret string) bool {
	var toggles []FeatureToggle
	if err := db.Where("key LIKE ?", strings.Split(key, keySeparator)[0]+"%").Find(&toggles).Error; err == nil {
		return len(toggles) != 0 && secret == toggles[0].Secret
	}
	return false
}

func init() {
	// Configure logger
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.DebugLevel)
}

func setupDatabase() {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		logger.Fatal("DB_DSN environment variable is required")
	}

	var err error
	db, err = initDatabase(dsn)
	if err != nil {
		logger.Fatal("failed to connect database after multiple attempts:", err)
	}
}

func main() {
	// Setup database connection
	setupDatabase()

	setupRouter().Run()
}

// setupRouter registers every route on a fresh engine. It is called by main()
// and by the tests, so both exercise the same handlers.
func setupRouter() *gin.Engine {
	router := gin.Default()

	// CORS: any origin, no credentials. The API is public and authenticates by
	// the secret in the path, never by cookies, so there is nothing for
	// credentials to carry. The previous echoed Origin plus Allow-Credentials
	// bought nothing, and lacking Vary: Origin it would have let a cache hand
	// one site's CORS header to another. A wildcard needs no Vary.
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	router.GET("/collectionHash/:key", func(c *gin.Context) {
		key := c.Param("key")
		logger.WithFields(logrus.Fields{
			"method": "GET",
			"path":   "/collectionHash/" + key,
			"key":    key,
		}).Info("Received GET request for collectionHash")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			if !startsWithUUID(key) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
				return
			}

			var collectionHash string
			if err := db.Raw(`
					SELECT encode(digest(string_agg(
                        key || ' ' || value || ' ' || COALESCE(active_at::text, '') || ' ' || COALESCE(disabled_at::text, '') || ' ' || COALESCE(array_to_string(tags, ','), ''),
                        ' ' ORDER BY key), 'sha256'::text), 'hex')
					FROM feature_toggles WHERE key LIKE ?;
				`, key+"%").Scan(&collectionHash).Error; err != nil {
				logger.WithFields(logrus.Fields{
					"method": "GET",
					"path":   "/collectionHash/" + key,
					"key":    key,
					"error":  err.Error(),
				}).Error("Failed to calculate collection hash")

				c.JSON(http.StatusNotFound, gin.H{"error": "Failed to calculate collection hash for provided UUID"})
				return
			} else {
				logger.WithFields(logrus.Fields{
					"method": "GET",

					"path":           "/collectionHash/" + key,
					"collectionHash": collectionHash,
				}).Info("Returning collectionHash")

				c.JSON(http.StatusOK, gin.H{
					"collectionHash": collectionHash,
				})
				return
			}
		}
	})

	router.GET("/features/:key", func(c *gin.Context) {
		key := c.Param("key")
		logger.WithFields(logrus.Fields{
			"method": "GET",
			"path":   "/features/" + key,
			"key":    key,
		}).Info("Received GET request for feature toggle")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			if !startsWithUUID(key) {
				c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
				return
			}
			var toggles []FeatureToggle
			
			// Check for tag filtering
			tagFilter := c.Query("tags")
			query := db.Where("key LIKE ?", key+"%")
			if tagFilter != "" {
				tags := strings.Split(tagFilter, ",")
				for _, tag := range tags {
					tag = strings.TrimSpace(tag)
					if tag != "" {
						query = query.Where("? = ANY(tags)", tag)
					}
				}
			}

			if err := query.Find(&toggles).Error; err != nil {
				logger.WithFields(logrus.Fields{
					"method": "GET",
					"path":   "/features/" + key,
					"key":    key,
					"error":  err.Error(),
				}).Error("Failed to find feature toggles")

				c.JSON(http.StatusNotFound, gin.H{"error": "No feature toggles found for provided UUID"})
				return
			} else {
				if len(toggles) == 0 {
					c.JSON(http.StatusNotFound, gin.H{"error": "No feature toggles found for provided UUID"})
					return
				}
				logger.WithFields(logrus.Fields{
					"method": "GET",
					"path":   "/features/" + key,
					"key":    key,
					"length": len(toggles),
				}).Info("Returning feature toggles without secrets")

				strippedToggles := make([]FeatureToggleDTO, 0, len(toggles))
				for _, obj := range toggles {
					strippedToggles = append(strippedToggles, toDTO(obj))
				}
				c.JSON(http.StatusOK, gin.H{
					"toggles": strippedToggles,
				})
			}
		} else {
			logger.WithFields(logrus.Fields{
				"method":     "GET",
				"path":       "/features/" + key,
				"key":        key,
				"value":      toggle.Value,
				"activeAt":   toggle.ActiveAt,
				"disabledAt": toggle.DisabledAt,
			}).Info("Returning feature toggle value without secret")

			c.JSON(http.StatusOK, toDTO(toggle))
		}
	})

	router.POST("/features", func(c *gin.Context) {
		var newToggle FeatureToggle
		var secret string = ""
		if err := c.ShouldBindJSON(&newToggle); err != nil {
			logger.WithFields(logrus.Fields{
				"method": "POST",
				"path":   "/features",
				"error":  err.Error(),
			}).Error("Failed to bind JSON for new feature toggle")

			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if newToggle.Value != "true" && newToggle.Value != "false" {
			logger.WithFields(logrus.Fields{
				"method": "POST",
				"path":   "/features",
				"key":    newToggle.Key,
				"value":  newToggle.Value,
			}).Error("Invalid value, returning 400")

			c.JSON(http.StatusBadRequest, gin.H{"error": `Invalid value, expected "true" or "false"`})
			return
		}

		// The UUID prefix is added below and counts towards the limit, so check
		// against the budget that is left for the caller-supplied part.
		maxKeyLength := MaxKeyLength
		if !startsWithUUID(newToggle.Key) {
			maxKeyLength -= len(uuid.New().String()) + len(keySeparator)
		}

		if len(newToggle.Key) > maxKeyLength {
			logger.WithFields(logrus.Fields{
				"method":    "POST",
				"path":      "/features",
				"keyLength": len(newToggle.Key),
				"maxLength": maxKeyLength,
			}).Error("Key too long, returning 400")

			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Key too long, maximum is %d characters", maxKeyLength)})
			return
		}

		if !startsWithUUID(newToggle.Key) {
			newToggle.Key = prependUUID(newToggle.Key)
			secret = generateSecret()
			newToggle.Secret = secret
		} else {
			if !secretsMatch(newToggle.Key, newToggle.Secret) {
				logger.WithFields(logrus.Fields{
					"method": "POST",
					"path":   "/features",
					"key":    newToggle.Key,
				}).Error("Invalid secret, returning 401")

				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
				return
			}
		}

		result := db.Create(&newToggle)
		if result.Error != nil {
			logger.WithFields(logrus.Fields{
				"method": "POST",
				"path":   "/features",
				"key":    newToggle.Key,
				"value":  newToggle.Value,
				"error":  result.Error.Error(),
			}).Error("Failed to create feature toggle")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create feature toggle"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method":   "POST",
			"path":     "/features",
			"key":      newToggle.Key,
			"value":    newToggle.Value,
			"activeAt": newToggle.ActiveAt,
		}).Info("Successfully created feature toggle")

		if secret != "" {
			// The one response that carries the secret: creating the first
			// toggle of a group is the only time the caller gets to see it.
			c.JSON(http.StatusCreated, struct {
				FeatureToggleDTO
				Secret string `json:"secret"`
			}{toDTO(newToggle), secret})
		} else {
			c.JSON(http.StatusCreated, toDTO(newToggle))
		}
	})

	router.PUT("/features/activate/:key/:secret", func(c *gin.Context) {
		key := c.Param("key")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/activate/" + key,
				"key":    key,
			}).Error("Invalid secret, returning 401")

			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "PUT",
			"path":   "/features/activate/" + key,
			"key":    key,
		}).Info("Received request to activate feature toggle")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/activate/" + key,
				"key":    key,
				"error":  err.Error(),
			}).Error("Failed to find feature toggle")

			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		toggle.Value = "true"

		if err := db.Save(&toggle).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method":   "PUT",
				"path":     "/features/activate/" + key,
				"key":      key,
				"activeAt": toggle.ActiveAt,
				"error":    err.Error(),
			}).Error("Failed to activate feature toggle")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to activate feature toggle"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method":   "PUT",
			"path":     "/features/activate/" + key,
			"key":      key,
			"activeAt": toggle.ActiveAt,
		}).Info("Successfully activated feature toggle")

		c.JSON(http.StatusOK, toDTO(toggle))
	})

	router.PUT("/features/activateAt/:key/:date/:secret", func(c *gin.Context) {
		key := c.Param("key")
		date := c.Param("date")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/activateAt/" + key + "/" + date,
				"key":    key,
			}).Error("Invalid secret, returning 401")

			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "PUT",
			"path":   "/features/activateAt/" + key + "/" + date,
			"key":    key,
		}).Info("Received request to activate feature toggle at")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/activateAt/" + key + "/" + date,
				"key":    key,
				"error":  err.Error(),
			}).Error("Failed to find feature toggle")

			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		parsed, err := time.Parse(time.RFC3339, date)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/activateAt/" + key + "/" + date,
				"key":    key,
				"date":   date,
				"error":  err.Error(),
			}).Error("Invalid date, returning 400")

			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date, expected RFC 3339 with offset"})
			return
		}

		toggle.ActiveAt = &parsed

		if err := db.Save(&toggle).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method":   "PUT",
				"path":     "/features/activateAt/" + key + "/" + date,
				"key":      key,
				"activeAt": toggle.ActiveAt,
				"error":    err.Error(),
			}).Error("Failed to activate feature toggle at")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to activate feature toggle at"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method":   "PUT",
			"path":     "/features/activateAt/" + key + "/" + date,
			"key":      key,
			"activeAt": toggle.ActiveAt,
		}).Info("Successfully set feature toggle activeAt")

		c.JSON(http.StatusOK, toDTO(toggle))
	})

	router.PUT("/features/deactivate/:key/:secret", func(c *gin.Context) {
		key := c.Param("key")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/deactivate/" + key,
				"key":    key,
			}).Error("Invalid secret, returning 401")

			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "PUT",
			"path":   "/features/deactivate/" + key,
			"key":    key,
		}).Info("Received request to deactivate feature toggle")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/deactivate/" + key,
				"key":    key,
				"error":  err.Error(),
			}).Error("Failed to find feature toggle")

			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		toggle.Value = "false"

		if err := db.Save(&toggle).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method":     "PUT",
				"path":       "/features/deactivate/" + key,
				"key":        key,
				"disabledAt": toggle.DisabledAt,
				"error":      err.Error(),
			}).Error("Failed to deactivate feature toggle")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to deactivate feature toggle"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method":     "PUT",
			"path":       "/features/deactivate/" + key,
			"key":        key,
			"disabledAt": toggle.DisabledAt,
		}).Info("Successfully deactivated feature toggle")

		c.JSON(http.StatusOK, toDTO(toggle))
	})

	router.PUT("/features/deactivateAt/:key/:date/:secret", func(c *gin.Context) {
		key := c.Param("key")
		date := c.Param("date")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/deactivateAt/" + key + "/" + date,
				"key":    key,
			}).Error("Invalid secret, returning 401")

			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "PUT",
			"path":   "/features/deactivateAt/" + key + "/" + date,
			"key":    key,
		}).Info("Received request to activate feature toggle at")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/deactivateAt/" + key + "/" + date,
				"key":    key,
				"error":  err.Error(),
			}).Error("Failed to find feature toggle")

			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		parsed, err := time.Parse(time.RFC3339, date)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/features/deactivateAt/" + key + "/" + date,
				"key":    key,
				"date":   date,
				"error":  err.Error(),
			}).Error("Invalid date, returning 400")

			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid date, expected RFC 3339 with offset"})
			return
		}

		toggle.DisabledAt = &parsed

		if err := db.Save(&toggle).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method":     "PUT",
				"path":       "/features/deactivateAt/" + key + "/" + date,
				"key":        key,
				"disabledAt": toggle.DisabledAt,
				"error":      err.Error(),
			}).Error("Failed to deactivate feature toggle at")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to deactivate feature toggle at"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method":     "PUT",
			"path":       "/features/deactivateAt/" + key + "/" + date,
			"key":        key,
			"disabledAt": toggle.DisabledAt,
		}).Info("Successfully set feature toggle disabledAt")

		c.JSON(http.StatusOK, toDTO(toggle))
	})

	router.DELETE("/features/:key/:secret", func(c *gin.Context) {
		key := c.Param("key")
		secret := c.Param("secret")

		if !secretsMatch(key, secret) {
			logger.WithFields(logrus.Fields{
				"method": "DELETE",
				"path":   "/features/" + key,
				"key":    key,
			}).Error("Invalid secret, returning 401")

			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "DELETE",
			"path":   "/features/" + key,
			"key":    key,
		}).Info("Received request to delete feature toggle")

		var toggle FeatureToggle
		if err := db.First(&toggle, "key = ?", key).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "DELETE",
				"path":   "/features/" + key,
				"key":    key,
				"error":  err.Error(),
			}).Error("Failed to find feature toggle")

			c.JSON(http.StatusNotFound, gin.H{"error": "Feature not found"})
			return
		}

		if err := db.Delete(&toggle).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "DELETE",
				"path":   "/features/" + key,
				"key":    key,
				"error":  err.Error(),
			}).Error("Failed to delete feature toggle")

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete feature toggle"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "DELETE",
			"path":   "/features/" + key,
			"key":    key,
		}).Info("Successfully deleted feature toggle")

		c.JSON(http.StatusOK, gin.H{"message": "Feature toggle deleted"})
	})

	router.PUT("/secret/update/:uuid/:oldsecret/:newsecret", func(c *gin.Context) {
		uuid := c.Param("uuid")
		oldSecret := c.Param("oldsecret")
		newSecret := c.Param("newsecret")

		if !secretsMatch(uuid+"|", oldSecret) {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/secret/update/" + uuid,
				"uuid":   uuid,
			}).Error("Invalid secret, returning 401")

			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid secret"})
			return
		}

		if !isURLParseable(newSecret) {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/secret/update/" + uuid,
				"uuid":   uuid,
			}).Error("New secret is not URL parseable, aborting operation")

			c.JSON(http.StatusNotAcceptable, gin.H{"error": "New secret is not URL parseable, aborting operation"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "PUT",
			"path":   "/secret/update/" + uuid,
			"uuid":   uuid,
		}).Info("Received request to update secret")

		if err := db.Model(&FeatureToggle{}).Where("key LIKE ?", uuid+"%").Update("secret", newSecret).Error; err != nil {
			logger.WithFields(logrus.Fields{
				"method": "PUT",
				"path":   "/secret/update/" + uuid,
				"error":  err.Error(),
			}).Error("Failed to update secret")

			c.JSON(http.StatusNotFound, gin.H{"error": "Failed to update secret"})
			return
		}

		logger.WithFields(logrus.Fields{
			"method": "PUT",
			"path":   "/secret/update/" + uuid,
			"key":    uuid,
		}).Info("Successfully updated secret")

		c.JSON(http.StatusOK, gin.H{
			"key": uuid,
		})
	})

	return router
}
