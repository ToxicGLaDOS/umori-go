package main

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt"
	"github.com/toxicglados/umori-go/pkg/crypto"
	"github.com/toxicglados/umori-go/pkg/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type AnyTime struct{} // I don't actually know if I even need this

func (a AnyTime) Match(v driver.Value) bool {
    _, ok := v.(time.Time)
    return ok
}

var (
	mock sqlmock.Sqlmock
	r *gin.Engine
	token string
	mulldrifter_id = "e0fed1e5-fcbd-4597-91b5-ba809571573b"
	black_lotus_id = "5089ec1a-f881-4d55-af14-5d996171203b"
)

type PasswordHash struct{}
func (ph PasswordHash) Match(v driver.Value) bool {
    hash, ok := v.(string)
		if !ok {
			log.Fatal("Failed to assert password hash as string")
		}
		// This _could_ be a whole regex, but this is sufficent
		return strings.HasPrefix(hash, "$argon2id")
}


func NewMockDB() (*gorm.DB, sqlmock.Sqlmock) {
    db, mock, err := sqlmock.New()
    if err != nil {
        log.Fatalf("An error '%s' was not expected when opening a stub database connection", err)
    }

		dialector := postgres.New(postgres.Config{
			Conn: db,
			DriverName: "postgres",
		})

    gormDB, err := gorm.Open(dialector, &gorm.Config{})

    if err != nil {
        log.Fatalf("An error '%s' was not expected when opening gorm database", err)
    }

    return gormDB, mock
}

func setup() {
	db, mock = NewMockDB()
	r = setupRouter()
	setupGoGuardian()
}

func TestRegister(t *testing.T) {

	mock.ExpectBegin()
	mock.ExpectQuery("^INSERT INTO \"users\" (.+)$").WithArgs(AnyTime{}, AnyTime{}, nil, "test", PasswordHash{}).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()

	w := callEndpoint(`{"username": "test", "password": "hunter2"}`, "POST", "/api/register")

	if w.Code != 200 {
		t.Fatalf("Expected status code 200, got \"%d\"", w.Code)
	}	
}

func TestRegisterTwice(t *testing.T) {
	mock.ExpectBegin()
	mock.ExpectQuery("^INSERT INTO \"users\" (.+)$").WithArgs(AnyTime{}, AnyTime{}, nil, "test", PasswordHash{}).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectQuery("^INSERT INTO \"users\" (.+)$").WithArgs(AnyTime{}, AnyTime{}, nil, "test", PasswordHash{}).WillReturnError(gorm.ErrDuplicatedKey)
	mock.ExpectRollback()


	w := callEndpoint(`{"username": "test", "password": "hunter2"}`, "POST", "/api/register")
	w = callEndpoint(`{"username": "test", "password": "hunter2"}`, "POST", "/api/register")

	expectedError := ErrorResponse{Message: ErrUserAlreadyExists.Error()}
	err := validateErrorResponse(w, 400, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterMissingPassword(t *testing.T) {
	w := callEndpoint(`{"username": "test"}`, "POST", "/api/register")

	expectedError := ErrorResponse{Message: models.ErrMissingPassword.Error()}
	err := validateErrorResponse(w, 400, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterMissingUsername(t *testing.T) {
	w := callEndpoint(`{"password": "hunter2"}`, "POST", "/api/register")

	expectedError := ErrorResponse{Message: models.ErrMissingUsername.Error()}
	err := validateErrorResponse(w, 400, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterEmptyBody(t *testing.T) {
	w := callEndpoint("", "POST", "/api/register")

	expectedError := ErrorResponse{Message: ErrUnexpectedEOF.Error()}
	err := validateErrorResponse(w, 400, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterMalformedData(t *testing.T) {
	w := callEndpoint("{foobar}", "POST", "/api/register")

	errorMessage := "invalid character 'f' looking for beginning of object key string"
	expectedError := ErrorResponse{Message: errorMessage}
	err := validateErrorResponse(w, 400, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegisterIncorrectDataType(t *testing.T) {
	w := callEndpoint("false", "POST", "/api/register")

	expectedError := ErrorResponse{Message: ErrInvalidJSON.Error()}
	err := validateErrorResponse(w, 400, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTokenEndpoint(t *testing.T) {
	passwordHash, err := crypto.GenerateFromPassword("hunter2", crypto.DefaultHashingParams())
	if err != nil {
		t.Fatalf("Couldn't hash password. Got error: \"%s\"", err)
	}

	mock.ExpectQuery(`^SELECT "password_hash" FROM "users" WHERE username = \$1 (.+)$`).WithArgs("test").WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(passwordHash))
	w := callEndpointWithBasicAuth("", "GET", "/api/token", "test", "hunter2")

	err = validateCode(w, 200)
	if err != nil {
		t.Fatal(err)
	}

	response := struct{Token *string}{}
	err = json.NewDecoder(w.Result().Body).Decode(&response)
	if err != nil {
		log.Fatal(err)
	}

	if response.Token == nil {
		log.Fatal("Response json didn't have the correct shape")
	}
	token = *response.Token
}

func TestTokenWithoutAuth(t *testing.T) {
	w := callEndpoint("", "GET", "/api/token")

	expectedError := ErrorResponse{Message: ErrMissingBasicAuth.Error()}
	err := validateErrorResponse(w, 401, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

// This user is already stored in cache from an earlier test
func TestTokenWithBadPassword(t *testing.T) {
	w := callEndpointWithBasicAuth("", "GET", "/api/token", "test", "wrong_password")

	expectedError := ErrorResponse{Message: ErrInvalidCredentials.Error()}
	err := validateErrorResponse(w, 401, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTokenWithNewUser(t *testing.T) {
	mock.ExpectQuery(`^SELECT "password_hash" FROM "users" WHERE username = \$1 (.+)$`).WithArgs("test2").WillReturnRows(sqlmock.NewRows([]string{"password_hash"}))

	w := callEndpointWithBasicAuth("", "GET", "/api/token", "test2", "hunter2")

	expectedError := ErrorResponse{Message: ErrInvalidCredentials.Error()}
	err := validateErrorResponse(w, 401, expectedError)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollectionUpdate(t *testing.T) {
	quantity := 5

	mock.ExpectQuery(`^SELECT "id" FROM "users" WHERE username = \$1 (.+)$`).WithArgs("test").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectQuery("^INSERT INTO \"collection_entries\" (.+) ON CONFLICT (.+)$").WithArgs(AnyTime{}, AnyTime{}, nil, 1, mulldrifter_id, quantity, quantity).WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "quantity"}).AddRow(1, mulldrifter_id, quantity))
	mock.ExpectCommit()

	body := fmt.Sprintf(`{"card_id": "%s", "quantity": %d}`, mulldrifter_id, quantity)
	w := callEndpointWithTokenAuth(body, "POST", "/api/test/collection/update", token)

	err := validateCode(w, 200)
	if err != nil {
		t.Fatal(err)
	}

	var collectionEntry models.CollectionEntry
	err = json.NewDecoder(w.Result().Body).Decode(&collectionEntry)
	if err != nil {
		t.Fatal(err)
	}

	if collectionEntry.CardID.String() != mulldrifter_id {
		t.Fatal("Card id didn't match")
	} else if collectionEntry.Quantity != quantity {
		t.Fatal("Quantity didnt' match")
	}
}

func TestCollectionUpdateWithMalformedToken(t *testing.T) {
	quantity := 5

	body := fmt.Sprintf(`{"card_id": "%s", "quantity": %d}`, mulldrifter_id, quantity)
	w := callEndpointWithTokenAuth(body, "POST", "/api/test/collection/update", "bad_token_format")

	errorResponse := ErrorResponse{Message: ErrMalformedToken.Error()}
	err := validateErrorResponse(w, 401, errorResponse)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollectionUpdateWithBadSignature(t *testing.T) {
	quantity := 5

	var hmacSampleSecret []byte
	hmacSampleSecret = []byte("incorrect signing secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": "test",
		"nbf": time.Date(2015, 10, 10, 12, 0, 0, 0, time.UTC).Unix(),
	})
	token.Header["kid"] = "secret-id"

	tokenString, err := token.SignedString(hmacSampleSecret)
	if err != nil {
		log.Fatal(err)
	}

	body := fmt.Sprintf(`{"card_id": "%s", "quantity": %d}`, mulldrifter_id, quantity)
	w := callEndpointWithTokenAuth(body, "POST", "/api/test/collection/update", tokenString)

	errorResponse := ErrorResponse{Message: ErrInvalidToken.Error()}
	err = validateErrorResponse(w, 401, errorResponse)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollectionUpdateWithNoKID(t *testing.T) {
	quantity := 5

	var hmacSampleSecret []byte
	hmacSampleSecret = []byte("incorrect signing secret")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user": "test",
		"nbf": time.Date(2015, 10, 10, 12, 0, 0, 0, time.UTC).Unix(),
	})

	tokenString, err := token.SignedString(hmacSampleSecret)
	if err != nil {
		log.Fatal(err)
	}

	body := fmt.Sprintf(`{"card_id": "%s", "quantity": %d}`, mulldrifter_id, quantity)
	w := callEndpointWithTokenAuth(body, "POST", "/api/test/collection/update", tokenString)

	errorResponse := ErrorResponse{Message: ErrMissingKID.Error()}
	err = validateErrorResponse(w, 401, errorResponse)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollectionUpdateWithWrongUsersToken(t *testing.T) {
	quantity := 5

	mock.ExpectBegin()
	mock.ExpectQuery("^INSERT INTO \"users\" (.+)$").WithArgs(AnyTime{}, AnyTime{}, nil, "test2", PasswordHash{}).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
	mock.ExpectCommit()


	var tokenResponse struct{Token string}
	// Get a token for the test2 user
	w := callEndpoint(`{"username": "test2", "password": "hunter2"}`, "POST", "/api/register")
	err := json.NewDecoder(w.Result().Body).Decode(&tokenResponse)
	if err != nil {
		log.Fatal(err)
	}

	body := fmt.Sprintf(`{"card_id": "%s", "quantity": %d}`, mulldrifter_id, quantity)
	// Try to use the test2 token for the test user
	w = callEndpointWithTokenAuth(body, "POST", "/api/test/collection/update", tokenResponse.Token)

	errorResponse := ErrorResponse{Message: ErrInvalidToken.Error()}
	err = validateErrorResponse(w, 401, errorResponse)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollectionGetByID(t *testing.T) {
	quantity := 5
	mock.ExpectQuery(`^SELECT (.+) FROM "collection_entries" (.+) WHERE (.+)$`).WithArgs("test", mulldrifter_id).WillReturnRows(sqlmock.NewRows([]string{"card_id", "user_id", "quantity"}).AddRow(mulldrifter_id, 1, quantity))

	endpoint := fmt.Sprintf("/api/test/collection/cards/%s", mulldrifter_id)
	w := callEndpointWithTokenAuth("", "GET", endpoint, token)

	err := validateCode(w, 200)
	if err != nil {
		t.Fatal(err)
	}

	var collectionEntries []models.CollectionEntry
	err = json.NewDecoder(w.Result().Body).Decode(&collectionEntries)
	if err != nil {
		t.Fatal(err)
	}

	collectionEntry := collectionEntries[0]
	if collectionEntry.CardID.String() != mulldrifter_id {
		t.Fatal("Card id didn't match")
	} else if collectionEntry.Quantity != quantity {
		t.Fatal("Quantity didnt' match")
	}
}

func TestCollectionGetByIDInvalidID(t *testing.T) {
	invalid_id := "not_a_uuid"

	endpoint := fmt.Sprintf("/api/test/collection/cards/%s", invalid_id)
	w := callEndpointWithTokenAuth("", "GET", endpoint, token)

	errorResponse := ErrorResponse{Message: ErrInvalidUUID.Error()}
	err := validateErrorResponse(w, 400, errorResponse)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCollectionGetByIDNoneInCollection(t *testing.T) {
	mock.ExpectQuery(`^SELECT (.+) FROM "collection_entries" (.+) WHERE (.+)$`).WithArgs("test", black_lotus_id).WillReturnRows(sqlmock.NewRows([]string{"card_id", "user_id", "quantity"}))

	endpoint := fmt.Sprintf("/api/test/collection/cards/%s", black_lotus_id)
	w := callEndpointWithTokenAuth("", "GET", endpoint, token)

	err := validateCode(w, 200)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(w.Result().Body)

	var collectionEntries []models.CollectionEntry
	err = json.Unmarshal(body, &collectionEntries)
	if err != nil {
		t.Fatal(err)
	}

	if len(collectionEntries) != 0 {
		t.Fatalf("Expected no entries in response, got %s", body)
	}
}

func callEndpointWithTokenAuth(payload, method, endpoint, token string) *httptest.ResponseRecorder {
	bodyReader := bytes.NewReader([]byte(payload))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, endpoint, bodyReader)
	req.Header.Add("Authorization", "Bearer "+ token)
	r.ServeHTTP(w, req)

	return w

}

func callEndpointWithBasicAuth(payload, method, endpoint, username, password string) *httptest.ResponseRecorder {
	bodyReader := bytes.NewReader([]byte(payload))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, endpoint, bodyReader)
	req.SetBasicAuth(username, password)
	r.ServeHTTP(w, req)

	return w
}


func callEndpoint(payload string, method string, endpoint string) *httptest.ResponseRecorder {
	bodyReader := bytes.NewReader([]byte(payload))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, endpoint, bodyReader)
	r.ServeHTTP(w, req)

	return w
}

func validateCode(w *httptest.ResponseRecorder, expectedCode int) error {
	if w.Code != expectedCode {
		errorMessage := fmt.Sprintf("Expected status code %d, got \"%d\"", expectedCode, w.Code)
		return errors.New(errorMessage)
	}
	return nil
}

func validateErrorResponse(w *httptest.ResponseRecorder, expectedCode int, expectedResponse interface{}) error {
	err := validateCode(w, expectedCode)
	if err != nil {
		return err
	}

	var response ErrorResponse
	err = json.NewDecoder(w.Result().Body).Decode(&response)
	if err != nil {
		return err
	}

	if response != expectedResponse{
		errorMessage := fmt.Sprintf("Expected \"%s\" as response, got \"%s\"", expectedResponse, response)
		return errors.New(errorMessage)
	}

	return nil
}

func TestMain(m *testing.M) {
	setup()
	code := m.Run()
	os.Exit(code)
}
