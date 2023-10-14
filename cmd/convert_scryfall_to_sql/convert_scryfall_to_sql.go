package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/toxicglados/umori-go/pkg/models"

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

func main() {
	defaultDataPath := os.Args[1]
	allDataPath := os.Args[2]
	content, err := os.ReadFile(defaultDataPath)
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

	content, err = os.ReadFile(allDataPath)
	if err != nil {
		log.Fatal("Error when opening file: ", err)
	}

	start = time.Now()
	var cards []models.Card
	err = json.Unmarshal(content, &cards)
	if err != nil {
			log.Fatal("Error during Unmarshal(): ", err)
	}
	end = time.Now()
	elapsed = end.Sub(start)
	fmt.Printf("Unmarshal all: %s\n", elapsed)

	for _, card := range cards {
		_, isDefault := defaultSet[card.ID.String()]
		card.DefaultLang = isDefault
	}

	var dsn = "host=localhost user=postgres password=password dbname=postgres port=55432 TimeZone=America/Chicago"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	db.AutoMigrate(&models.Card{}, &models.Set{}, &models.Face{}, &models.Finish{})

	start = time.Now()

	// We have to delete everything from these tables cause
	// the JSON from scryfall doesn't have anything to primary_key
	// off of to make upsert work
	db.Unscoped().Where("1 = 1").Delete(&models.Finish{})
	db.Unscoped().Where("1 = 1").Delete(&models.Face{})

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
