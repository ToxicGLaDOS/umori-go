package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/toxicglados/umori-go/pkg/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const (
	pageSize = 25
)

var dsn = "host=localhost user=postgres password=password dbname=postgres port=55432 TimeZone=America/Chicago"
var db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})

func Paginate(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	return func (db *gorm.DB) *gorm.DB {
		page, err := strconv.Atoi(c.Query("page"))
		if err != nil || page < 0 {
			page = 0
		}
		offset := page * pageSize

		return db.Offset(offset).Limit(pageSize)
	}
}

func setupRouter() *gin.Engine {
	// Disable Console Color
	// gin.DisableConsoleColor()
	r := gin.Default()

	// Ping test
	r.GET("/api/cards/search", func(c *gin.Context) {
		// nameContains is never empty because of the %%
		// so even without a parameter we will search for everything
		nameContains := fmt.Sprintf("%%%s%%", c.Query("nameContains"))

		var cards []models.Card
		result := db.Model(&models.Card{}).
		             Preload("Set").
		             Preload("Finishes").
		             Preload("Faces").
		             Where("name ILIKE ?", nameContains).
		             Order("name, id").
		             Scopes(Paginate(c)).
		             Find(&cards)

		if result.Error != nil {
			log.Fatal(result.Error)
		}
		c.JSON(http.StatusOK, cards)
	})

	return r
}

func main() {
	r := setupRouter()
	// Listen and Server in 0.0.0.0:8080
	r.Run(":8080")
}
