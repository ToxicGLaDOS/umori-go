package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"time"

	"database/sql/driver"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func splitManaCost(manaCost string) ([]string, error){
	var manaSymbols []string
	var symbolAccumulator string = ""

	for _, c := range manaCost {
		char := string(c)
		symbolAccumulator = symbolAccumulator + char
		if char == "}" {
			manaSymbols = append(manaSymbols, string(symbolAccumulator))
			symbolAccumulator = ""
		}
	}

	// TODO: Consider validating the mana symbol against a list of known ones
	if symbolAccumulator != "" {
		return nil, errors.New("Error parsing mana cost, unmatched {")
	}

	return manaSymbols, nil
}

type DefaultCard struct {	
	ID uuid.UUID `json:"id"`
}

type Face struct {
	gorm.Model
	Name string `json:"name"`
	ImageURIs ImageURIs `json:"image_uris" gorm:"embedded"`
	CardID uuid.UUID
}

type JSONCard struct {
	ID uuid.UUID `json:"id"`
	Name string `json:"name"`
	URI JSONURL `json:"uri"`
	ImageURIs ImageURIs `json:"image_uris"`
	Faces []Face `json:"card_faces"`
	SetID uuid.UUID `json:"set_id"`
	Finishes []string `json:"finishes"`
	SetName string `json:"set_name"`
	SetType string `json:"set_type"`
	SetCode string `json:"set"` // The (usually) 3 letter code
	CollectorNumber string `json:"collector_number"`
	Layout string `json:"layout"`
	Foil bool `json:"foil"`
}

type Finish struct {
	gorm.Model
	Name string
	CardID uuid.UUID
}

type Card struct {
	gorm.Model
	ID uuid.UUID `gorm:"type:uuid;primary_key"`
	Name string
	URI JSONURL `gorm:"embedded"`
	ImageURIs ImageURIs `gorm:"embedded"`
	Faces []Face
	Finishes []Finish
	DefaultLang bool
	SetID uuid.UUID
	Set Set
	Layout string
	CollectorNumber string
	Foil bool
}

type JSONURL struct {
	url.URL
}

type ImageURIs struct {
	Small JSONURL `json:"small"`
	Normal JSONURL `json:"normal"`
	Large JSONURL `json:"large"`
	PNG JSONURL `json:"png"`
	ArtCrop JSONURL `json:"art_crop"`
	BorderCrop JSONURL `json:"border_crop"`
}

type Set struct {
	gorm.Model
	ID uuid.UUID `gorm:"type:uuid;primary_key"`
	Name string
	Type string
	Code string `gorm:"unique"` // The (usually) 3 letter code
}

func (j *JSONURL) UnmarshalJSON(data []byte) error {
	url, err := url.Parse(string(data[1 : len(data)-1]))
	if err != nil {
		return err
	}

	j.URL = *url
	return nil
}

func (j *JSONURL) Scan(src interface{}) error {
	url, err := url.Parse(string(src.([]byte)))
	if err != nil {
		return err
	}

	j.URL = *url
	return nil
}

func (j JSONURL) Value() (driver.Value, error) {
	return j.URL.String(), nil
}

func(card *Card) UnmarshalJSON(data []byte) error {
	var jsonCard JSONCard
	json.Unmarshal(data, &jsonCard)

	var finishes []Finish
	for _, finish := range jsonCard.Finishes {
		finishes = append(finishes, Finish{Name: finish})
	}

	card.ID = jsonCard.ID
	card.Name = jsonCard.Name
	card.URI = jsonCard.URI
	card.CollectorNumber = jsonCard.CollectorNumber
	card.Faces = jsonCard.Faces
	card.Layout = jsonCard.Layout
	card.Foil = jsonCard.Foil
	card.Finishes = finishes
	card.ImageURIs = ImageURIs{
		Small: jsonCard.ImageURIs.Small,
		Normal: jsonCard.ImageURIs.Normal,
		Large: jsonCard.ImageURIs.Large,
		PNG: jsonCard.ImageURIs.PNG,
		ArtCrop: jsonCard.ImageURIs.ArtCrop,
		BorderCrop: jsonCard.ImageURIs.BorderCrop,
	}
	card.Set = Set{
		ID: jsonCard.SetID,
		Name: jsonCard.SetName,
		Type: jsonCard.SetType,
		Code: jsonCard.SetCode,
	}

	return nil
}

func main() {
	content, err := ioutil.ReadFile("./default-cards-20231007210701.json")
	if err != nil {
		log.Fatal("Error when opening file: ", err)
	}

	start := time.Now()
	var defaultCards []DefaultCard
	err = json.Unmarshal(content, &defaultCards)
	if err != nil {
			log.Fatal("Error during Unmarshal(): ", err)
	}
	end := time.Now()
	elapsed := end.Sub(start)
	fmt.Printf("Unmarshal default: %s\n", elapsed)

	// While this is a map type, we use it as a quick lookup
	// for whether an ID exists in the defaultSet or not
	defaultSet := make(map[string]bool)

	for _, jsonCard := range defaultCards {
		defaultSet[jsonCard.ID.String()] = true
	}

	content, err = ioutil.ReadFile("./all-cards-20231007212054.json")
	if err != nil {
		log.Fatal("Error when opening file: ", err)
	}

	start = time.Now()
	var cards []Card
	err = json.Unmarshal(content, &cards)
	if err != nil {
			log.Fatal("Error during Unmarshal(): ", err)
	}
	end = time.Now()
	elapsed = end.Sub(start)
	fmt.Printf("Unmarshal all: %s\n", elapsed)

	start = time.Now()
	for _, card := range cards {
		_, isDefault := defaultSet[card.ID.String()]
		card.DefaultLang = isDefault
	}
	end = time.Now()
	elapsed = end.Sub(start)
	fmt.Printf("Convert all: %s\n", elapsed)


	var dsn = "host=localhost user=postgres password=password dbname=postgres port=55432 TimeZone=America/Chicago"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	db.AutoMigrate(&Card{}, &Set{}, &Face{}, &Finish{})

	start = time.Now()

	db.Unscoped().Where("1 = 1").Delete(&Finish{}).Delete(&Face{}).Delete(&Card{})

	result := db.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).CreateInBatches(cards, 1000)

	if result.Error != nil {
		log.Fatal(result.Error)
	}
	end = time.Now()
	elapsed = end.Sub(start)
	fmt.Printf("Save all: %s\n", elapsed)
}
