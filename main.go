package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	//"github.com/google/uuid"
	"github.com/toxicglados/umori-go/pkg/crypto"
	"github.com/toxicglados/umori-go/pkg/models"

	"github.com/gin-gonic/gin"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	"github.com/shaj13/go-guardian/v2/auth/strategies/basic"
	//"github.com/shaj13/go-guardian/v2/auth/strategies/token"
	"github.com/shaj13/go-guardian/v2/auth/strategies/union"
	"github.com/shaj13/libcache"
	_ "github.com/shaj13/libcache/fifo"
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

var (
	strategy union.Union
	keeper jwt.SecretsKeeper
	tokenStrategy auth.Strategy
	cacheObj libcache.Cache
)

func setupRouter() *gin.Engine {
	// Disable Console Color
	// gin.DisableConsoleColor()
	r := gin.Default()

	authorized := r.Group("/api")
	authorized.Use(AuthRequired())
	{
		authorized.GET("/test", test)
	}

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

	r.POST("/api/register", func(c *gin.Context) {
		var user models.User

		err := c.BindJSON(&user)
		if err != nil {
			log.Fatal(err)
			return
		}

		db.Create(&user)

		c.JSON(http.StatusOK, struct{}{})
	})

	r.POST("/api/login", login)


	return r
}

func test(c *gin.Context) {
	token := createToken(c.Request)

	c.JSON(http.StatusOK, struct{Token string}{Token: token})
}

func login(c *gin.Context) {
	var user models.UnsafeUser

	err := c.BindJSON(&user)
	if err != nil {
		log.Fatal(err)
		return
	}

	var dbUser models.User
	result := db.Select("PasswordHash").Where("username = ?", user.Username).First(&dbUser)

	if result.Error != nil {
		log.Fatal(result.Error)
	}


	match, err := crypto.ComparePasswordAndHash(user.Password, dbUser.PasswordHash)

	if !match {
		c.JSON(http.StatusOK, struct{Message string}{Message: "Password didn't match"})
		return
	}
	
	c.JSON(http.StatusOK, struct{Message string}{Message: "Password matched"})
}

func setupGoGuardian() {
	keeper = jwt.StaticSecret{
		ID:        "secret-id",
		Secret:    []byte("secret"),
		Algorithm: jwt.HS256,
	}
	cacheObj = libcache.FIFO.New(0)
	cacheObj.SetTTL(time.Minute * 1)
	basicStrategy := basic.NewCached(validateUser, cacheObj)
	jwtStrategy := jwt.New(cacheObj, keeper)
	strategy = union.New(jwtStrategy, basicStrategy)
}

func createToken(r *http.Request) string {
	u := auth.User(r)
	token, _ := jwt.IssueAccessToken(u, keeper)
	log.Println(u.GetUserName())
	return token
}

func validateUser(ctx context.Context, r *http.Request, userName, password string) (auth.Info, error) {
	var dbUser models.User
	result := db.Select("PasswordHash").Where("username = ?", userName).First(&dbUser)
	if result.Error != nil {
		log.Fatal(result.Error)
	}


	match, err := crypto.ComparePasswordAndHash(password, dbUser.PasswordHash)
	if err != nil {
		log.Fatal(err)
	}

	if match {
		return auth.NewDefaultUser(userName, "1", nil, nil), nil
	}

	return nil, fmt.Errorf("Invalid credentials")
}

func AuthRequired() gin.HandlerFunc{
	return func(c *gin.Context) {
		_, user, err := strategy.AuthenticateRequest(c.Request)
		if err != nil {
			fmt.Println(err)
			c.JSON(http.StatusUnauthorized, struct{}{})
			return
		}
		log.Printf("User %s Authenticated\n", user.GetUserName())

		// This is critical because we pull the user out in
		// createToken
		c.Request = auth.RequestWithUser(user, c.Request)
		c.Next()
	}
}

func main() {
	db.AutoMigrate(&models.Card{},
	               &models.Set{},
								 &models.Face{},
								 &models.Finish{},
							   &models.User{})

	setupGoGuardian()

	r := setupRouter()	
	// Listen and Server in 0.0.0.0:8080
	r.Run(":8080")
}
