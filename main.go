package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/toxicglados/umori-go/pkg/crypto"
	"github.com/toxicglados/umori-go/pkg/models"

	"github.com/gin-gonic/gin"
	"github.com/shaj13/go-guardian/v2/auth"
	"github.com/shaj13/go-guardian/v2/auth/strategies/basic"
	"github.com/shaj13/go-guardian/v2/auth/strategies/jwt"
	"github.com/shaj13/libcache"
	_ "github.com/shaj13/libcache/fifo"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	pageSize = 25
)

var (
	db *gorm.DB
	dsn = "host=localhost user=postgres password=password dbname=postgres port=55432 TimeZone=America/Chicago"
	ErrUnexpectedEOF error = errors.New("Unexpected EOF")
	ErrUserAlreadyExists error = errors.New("That username already exists")
	ErrUnknown error = errors.New("Unknown error")
	ErrCreatingToken error = errors.New("Error creating token")
	ErrInvalidJSON error = errors.New("Invalid JSON body")

)
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
		c.Set("offset", int64(offset))
		return db.Offset(offset).Limit(pageSize)
	}
}

type SearchResult struct {
	PagedResult
	Cards []models.Card `json:"results"`
}

type PagedResult struct {
	HasMore bool `json:"has_more"`
}

func NewPagedResult(count, offset int64) PagedResult {
	return PagedResult {
		HasMore: count - (offset + pageSize) > 0,
	}
}

type ErrorResponse struct {
	Message string `json:"message"`
}

var (
	jwtStrategy auth.Strategy
	basicStrategy auth.Strategy
	keeper jwt.SecretsKeeper
	cacheObj libcache.Cache
	unauthorizedResponse ErrorResponse = ErrorResponse{Message: "Unauthorized"}
)

func setupRouter() *gin.Engine {
	// Disable Console Color
	// gin.DisableConsoleColor()
	r := gin.Default()

	basicAuthorized := r.Group("/api")
	basicAuthorized.Use(BasicAuthRequired())
	{
		basicAuthorized.GET("/token", tokenEndpoint)
	}

	tokenAuthorized := r.Group("/api")
	tokenAuthorized.Use(TokenAuthRequired())
	{
		tokenAuthorized.POST("/:user/collection/:action", collectionPostEndpoint)
		tokenAuthorized.GET("/:user/collection/cards/:id", collectionGetCardsByID)
		// Token auth functions go here
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
		offset, exists := c.Get("offset")
		if !exists {
			log.Fatal(errors.New("Couldn't find offset in context"))
		}

		pagedSearchResult := SearchResult{
			PagedResult: NewPagedResult(count, offset.(int64)),
			Cards: cards,
		}
		c.JSON(http.StatusOK, pagedSearchResult)
	})

	r.POST("/api/register", func(c *gin.Context) {
		var user models.User
	
		err := c.BindJSON(&user)
		if err != nil {
			if errors.Is(err, io.EOF) {
				c.JSON(http.StatusBadRequest, ErrorResponse{Message: ErrUnexpectedEOF.Error()})
			} else {
				var unmarshalTypeError *json.UnmarshalTypeError
				var syntaxError *json.SyntaxError
				if errors.As(err, &unmarshalTypeError) {
					c.JSON(http.StatusBadRequest, ErrorResponse{Message: ErrInvalidJSON.Error()})
				} else if errors.Is(err, models.ErrMissingPassword) || errors.Is(err, models.ErrMissingUsername) || errors.As(err, &syntaxError) {	
					c.JSON(http.StatusBadRequest, ErrorResponse{Message: err.Error()})
				} else {
					c.JSON(http.StatusBadRequest, ErrorResponse{Message: ErrUnknown.Error()})
				}
			}
			return
		}

		err = db.Create(&user).Error

		if err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				c.JSON(http.StatusBadRequest, ErrorResponse{Message: ErrUserAlreadyExists.Error()})
			} else {
				// I'm not sure what could cause this
				log.Printf("Got unexpected error during user creation: \"%s\"\n", err.Error())
				c.JSON(http.StatusBadRequest, ErrorResponse{Message: ErrUnknown.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, struct{}{})
	})

	return r
}

type UpdateRequest struct {
	CardID uuid.UUID `json:"card_id"`
	Quantity int `json:"quantity"`
}

func updateCollection(c *gin.Context) {
	var dbUser models.User
	db.Model(&models.User{}).Select("id").Where("username = ?", c.Param("user")).First(&dbUser)

	var updateRequest UpdateRequest
	c.BindJSON(&updateRequest)
	collectionEntry := models.CollectionEntry {
		UserID: dbUser.ID,
		CardID: updateRequest.CardID,
		Quantity: updateRequest.Quantity,
	}

	// This is kind of complicated so here's the explanation
	// We create the collectionEntry, but on a conflict we
	// add the quantity of the existing column to the quantity
	// we were given. This gets wrapped in GREATEST(x, 0)
	// so it doesn't go below 0 clause.Returning{} ensures that we
	// put the value after resolving the conflict into &collectionEntry
	result := db.Clauses(
		clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "card_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"quantity": gorm.Expr("GREATEST(collection_entries.quantity + ?, 0)", collectionEntry.Quantity)})},
		clause.Returning{},
	).Create(&collectionEntry)
	if result.Error != nil {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Message: "Error creating db entry"})
		return
	}

	c.JSON(http.StatusOK, collectionEntry)
}

func collectionGetCardsByID(c *gin.Context) {
	id := c.Param("id")
	username := c.Param("user")

	var collectionEntries []models.CollectionEntry
	err := db.Model(&models.CollectionEntry{}).
	          Joins("left join users on users.id = collection_entries.user_id").
	          Where("username = ?", username).
	          Where("card_id = ?", id).
	          Find(&collectionEntries).
	          Error
	if err != nil {
		log.Fatal(err)
	}

	c.JSON(http.StatusOK, collectionEntries)
}

func collectionPostEndpoint(c *gin.Context) {
	action := c.Param("action")
	if action == "update" {
		updateCollection(c)
	} else {
		errorMessage := fmt.Sprintf("Unknown action: %s", action)
		c.JSON(http.StatusBadRequest, ErrorResponse{Message: errorMessage})
	}
}

func tokenEndpoint(c *gin.Context) {
	token, err := createToken(c.Request)
	if err != nil {
		log.Printf("Error creating token: \"%s\"\n", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Message: ErrCreatingToken.Error()})
		return
	}

	c.JSON(http.StatusOK, struct{Token string}{Token: token})
}

func setupGoGuardian() {
	keeper = jwt.StaticSecret{
		ID:        "secret-id",
		Secret:    []byte("secret"),
		Algorithm: jwt.HS256,
	}
	cacheObj = libcache.FIFO.New(0)
	cacheObj.SetTTL(time.Minute * 1)
	basicStrategy = basic.NewCached(validateUser, cacheObj)
	jwtStrategy = jwt.New(cacheObj, keeper)
}

func createToken(r *http.Request) (string, error) {
	u := auth.User(r)
	token, err := jwt.IssueAccessToken(u, keeper)
	return token, err
}

// Only called if user isn't found in cacheObj
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
		// I honestly don't know what the second parameter
		// "id" does here. Keeping it as a literal "1" didn't
		// seem to matter, but we set it to the userID just to
		// ensure it's different per user in case that matters
		return auth.NewDefaultUser(userName, strconv.FormatUint(uint64(dbUser.ID), 10), nil, nil), nil
	}

	// This is a little weird because validateUser doesn't have
	// anything to do with basic auth necessarily, but that's all
	// we use to call this function.
	// This makes it so the errors are the same whether the
	// user was found in cache or not
	return nil, basic.ErrInvalidCredentials
}

func BasicAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, err := basicStrategy.Authenticate(c.Request.Context(), c.Request)
		if err != nil {
			fmt.Printf("got error when authenticating: %s\n", err.Error())
			if errors.Is(err, basic.ErrMissingPrams) {
				errorResponse := ErrorResponse{
					Message: "Request missing BasicAuth",
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse)
			} else if errors.Is(err, basic.ErrInvalidCredentials) {
				errorResponse := ErrorResponse{
					Message: "Invalid credentials",
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse)
			} else {
				errorResponse := ErrorResponse{
					Message: err.Error(),
				}
				c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse)
			}

			return
		}

		// This is critical because we pull the user out in
		// createToken
		c.Request = auth.RequestWithUser(user, c.Request)
		c.Next()
	}
}

func TokenAuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, err := jwtStrategy.Authenticate(c.Request.Context(), c.Request)
		if err != nil {
			fmt.Printf("got error when authenticating: %s\n", err)
			errorResponse := ErrorResponse{
				Message: err.Error(),
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse)
			return
		}

		username := c.Param("user")

		if user.GetUserName() != username {
			c.AbortWithStatusJSON(http.StatusUnauthorized, unauthorizedResponse)
			return
		}

		// This is critical because we pull the user out in
		// createToken
		c.Request = auth.RequestWithUser(user, c.Request)
		c.Next()
	}
}

func main() {
	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatal(err)
	}

	err = db.AutoMigrate(&models.Card{},
	                      &models.Set{},
	                      &models.Face{},
	                      &models.Finish{},
	                      &models.User{},
	                      &models.CollectionEntry{})
	if err != nil {
		log.Fatal(err)
	}

	setupGoGuardian()

	r := setupRouter()	
	// Listen and Server in 0.0.0.0:8080
	r.Run(":8080")
}
