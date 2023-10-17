package main

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
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
	mock.ExpectQuery("^INSERT INTO \"users\" (.+)$").WithArgs(AnyTime{}, AnyTime{}, nil, "test", PasswordHash{}).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("1"))
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
