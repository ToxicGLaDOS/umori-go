package models

import (
	"encoding/json"

	"gorm.io/gorm"
	"github.com/google/uuid"
	"github.com/toxicglados/umori-go/pkg/jsonurl"
)

type Face struct {
	gorm.Model `json:"-"`
	Name string `json:"name"`
	ImageURIs ImageURIs `json:"image_uris" gorm:"embedded"`
	CardID uuid.UUID `json:"-"`
}

type Finishes []Finish
type Finish struct {
	gorm.Model `json:"-"`
	Name string `json:"name"`
	CardID uuid.UUID `json:"-"`
}

type Card struct {
	gorm.Model `json:"-"`
	ID uuid.UUID `json:"id" gorm:"type:uuid;primary_key"`
	Name string `json:"name"`
	URI jsonurl.JSONURL `json:"uri" gorm:"embedded"`
	ImageURIs ImageURIs `json:"image_uris" gorm:"embedded"`
	Faces []Face `json:"faces"`
	Finishes Finishes `json:"finishes"`
	DefaultLang bool `json:"default_lang"`
	SetID uuid.UUID  `json:"-"`
	Set Set `json:"set"`
	Layout string `json:"layout"`
	CollectorNumber string `json:"collector_number"`
}

type Set struct {
	gorm.Model `json:"-"`
	ID uuid.UUID `json:"id" gorm:"type:uuid;primary_key"`
	Name string `json:"name"`
	Type string `json:"type"`
	Code string `json:"code" gorm:"unique"` // The (usually) 3 letter code
}

type ImageURIs struct {
	Small jsonurl.JSONURL `json:"small"`
	Normal jsonurl.JSONURL `json:"normal"`
	Large jsonurl.JSONURL `json:"large"`
	PNG jsonurl.JSONURL `json:"png"`
	ArtCrop jsonurl.JSONURL `json:"art_crop"`
	BorderCrop jsonurl.JSONURL `json:"border_crop"`
}

type ScryfallCard struct {
	ID uuid.UUID `json:"id"`
	Name string `json:"name"`
	URI jsonurl.JSONURL `json:"uri"`
	ImageURIs ImageURIs `json:"image_uris"`
	Faces []Face `json:"card_faces"`
	SetID uuid.UUID `json:"set_id"`
	Finishes []string `json:"finishes"`
	SetName string `json:"set_name"`
	SetType string `json:"set_type"`
	SetCode string `json:"set"` // The (usually) 3 letter code
	CollectorNumber string `json:"collector_number"`
	Layout string `json:"layout"`
}

func(finishes Finishes) MarshalJSON() ([]byte, error) {
	var stringFinishes []string
	for _, finish := range finishes {
		stringFinishes = append(stringFinishes, finish.Name)
	}

	return json.Marshal(stringFinishes)
}

func(card *Card) UnmarshalJSON(data []byte) error {
	var jsonCard ScryfallCard
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
