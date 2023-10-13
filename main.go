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

func GetOffset(c *gin.Context) int {
	page, err := strconv.Atoi(c.Query("page"))
	if err != nil || page < 0 {
		page = 0
	}
	offset := page * pageSize

	return offset
}

func Paginate(c *gin.Context) func(db *gorm.DB) *gorm.DB {
	return func (db *gorm.DB) *gorm.DB {
		offset := GetOffset(c)
		return db.Offset(offset).Limit(pageSize)
	}
}

type PagedSearchResult struct {
	HasMore bool `json:"has_more"`
	Cards []models.Card `json:"results"`
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

		var count int64
		var cards []models.Card
		result := db.Model(&models.Card{}).
		             Preload("Set").
		             Preload("Finishes").
		             Preload("Faces").
		             Where("name ILIKE ?", nameContains).
		             Order("name, id").
		             Count(&count).
		             Scopes(Paginate(c)).
		             Find(&cards)

		if result.Error != nil {
			log.Fatal(result.Error)
		}

		pagedSearchResult := PagedSearchResult{
			HasMore: count - (int64(GetOffset(c)) + pageSize) > 0,
			Cards: cards,
		}
		c.JSON(http.StatusOK, pagedSearchResult)
	})

	return r
}

func main() {
	r := setupRouter()
	// Listen and Server in 0.0.0.0:8080
	r.Run(":8080")
}
