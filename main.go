package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/toxicglados/umori-go/pkg/models"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)


var dsn = "host=localhost user=postgres password=password dbname=postgres port=55432 TimeZone=America/Chicago"
var db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})

func setupRouter() *gin.Engine {
	// Disable Console Color
	// gin.DisableConsoleColor()
	r := gin.Default()

	// Ping test
	r.GET("/api/cards/search", func(c *gin.Context) {
		nameContains := c.Query("nameContains")
		nameContains = fmt.Sprintf("%%%s%%", nameContains)
		var cards []models.Card
		if len(nameContains) > 0 {
			result := db.Model(&models.Card{}).Preload("Set").Preload("Finishes").Preload("Faces").Where("name ILIKE ?", nameContains).Limit(1).Find(&cards)
			if result.Error != nil {
				log.Fatal(result.Error)
			}
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
