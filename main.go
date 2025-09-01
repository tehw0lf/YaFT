package main

import (
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

type FeatureToggle struct {
	ID         uint           `gorm:"primaryKey"`
	Key        string         `gorm:"unique;not null"`
	Value      string         `gorm:"not null"`
	ActiveAt   *time.Time     `gorm:"null"`
	DisabledAt *time.Time     `gorm:"null"`
	Secret     string         `gorm:"null"`
	Tags       pq.StringArray `gorm:"type:text[]"`
}

type FeatureToggleDTO struct {
	Key        string
	Value      string
	ActiveAt   *time.Time
	DisabledAt *time.Time
	Tags       pq.StringArray
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
	return newUUID + "|" + key
}

func startsWithUUID(key string) bool {
	firstPart := strings.Split(key, "|")[0]
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
	if err := db.Where("key LIKE ?", strings.Split(key, "|")[0]+"%").Find(&toggles).Error; err == nil {
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
	
	router := gin.Default()

	// Add CORS middleware
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
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

				var strippedToggles []FeatureToggleDTO
				for _, obj := range toggles {
					newObj := FeatureToggleDTO{
						Key:        obj.Key,
						Value:      obj.Value,
						ActiveAt:   obj.ActiveAt,
						DisabledAt: obj.DisabledAt,
						Tags:       obj.Tags,
					}
					strippedToggles = append(strippedToggles, newObj)
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

			c.JSON(http.StatusOK, gin.H{
				"key":        toggle.Key,
				"value":      toggle.Value,
				"activeAt":   toggle.ActiveAt,
				"disabledAt": toggle.DisabledAt,
				"tags":       toggle.Tags,
			})
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
			c.JSON(http.StatusCreated, gin.H{
				"key":        newToggle.Key,
				"value":      newToggle.Value,
				"activeAt":   newToggle.ActiveAt,
				"disabledAt": newToggle.DisabledAt,
				"tags":       newToggle.Tags,
				"secret":     secret,
			})
		} else {
			c.JSON(http.StatusCreated, gin.H{
				"key":        newToggle.Key,
				"value":      newToggle.Value,
				"activeAt":   newToggle.ActiveAt,
				"disabledAt": newToggle.DisabledAt,
				"tags":       newToggle.Tags,
			})
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

		c.JSON(http.StatusOK, gin.H{
			"key":        toggle.Key,
			"value":      toggle.Value,
			"activeAt":   toggle.ActiveAt,
			"disabledAt": toggle.DisabledAt,
			"tags":       toggle.Tags,
		})
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

		*toggle.ActiveAt, _ = time.Parse(time.RFC3339, date)

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

		c.JSON(http.StatusOK, gin.H{
			"key":        toggle.Key,
			"value":      toggle.Value,
			"activeAt":   toggle.ActiveAt,
			"disabledAt": toggle.DisabledAt,
			"tags":       toggle.Tags,
		})
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

		c.JSON(http.StatusOK, gin.H{
			"key":        toggle.Key,
			"value":      toggle.Value,
			"activeAt":   toggle.ActiveAt,
			"disabledAt": toggle.DisabledAt,
			"tags":       toggle.Tags,
		})
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

		*toggle.DisabledAt, _ = time.Parse(time.RFC3339, date)

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

		c.JSON(http.StatusOK, gin.H{
			"key":        toggle.Key,
			"value":      toggle.Value,
			"activeAt":   toggle.ActiveAt,
			"disabledAt": toggle.DisabledAt,
			"tags":       toggle.Tags,
		})
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

	router.Run()
}
