// Package appdomains holds the app domains table (app-domains v1): what an app is
// for, read from its app.yml tags, with the colours and icon its banner is drawn
// in. The same file is published, from this repo's terraform, at its $id:
// https://schemas.pennsieve.io/app-domains/v1.json. The web app's Apps tab and
// github-service's Discover banners read it, so a card and its Discover page match.
package appdomains

import (
	_ "embed"
	"encoding/json"
)

// URL is the table's $id, where it's published.
const URL = "https://schemas.pennsieve.io/app-domains/v1.json"

//go:embed app-domains.v1.json
var V1 []byte

type Lightness struct {
	S float64 `json:"s"`
	L float64 `json:"l"`
}

// Palette is a domain class's colours, as saturation and lightness on the domain's hue.
type Palette struct {
	Hue    float64    `json:"hue,omitempty"`
	Bg     Lightness  `json:"bg"`
	Tile   Lightness  `json:"tile"`
	Ink    Lightness  `json:"ink"`
	Line   Lightness  `json:"line"`
	Accent *Lightness `json:"accent"`
}

type Domain struct {
	Key     string   `json:"key"`
	Subject bool     `json:"subject"`
	Label   string   `json:"label"`
	Tag     string   `json:"tag"`
	Hue     float64  `json:"hue"`
	Aliases []string `json:"aliases"`
	Icon    string   `json:"icon"`
}

type Field struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Tag     string   `json:"tag"`
	Aliases []string `json:"aliases"`
}

type Table struct {
	ID     string `json:"$id"`
	Colors struct {
		Subject Palette `json:"subject"`
		Task    Palette `json:"task"`
		Neutral Palette `json:"neutral"`
	} `json:"colors"`
	Domains []Domain `json:"domains"`
	Fields  []Field  `json:"fields"`
}

// Load reads the embedded table.
func Load() (Table, error) {
	var t Table
	err := json.Unmarshal(V1, &t)
	return t, err
}
