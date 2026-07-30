package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"strings"
)

const (
	helpCardWidth  = 1448
	helpCardHeight = 1086
)

//go:embed assets/help-cards.json
var helpCatalogJSON []byte

//go:embed assets/help-broadcast.png
var helpBroadcastPNG []byte

//go:embed assets/help-member-alarm-news.png
var helpMemberAlarmNewsPNG []byte

//go:embed assets/help-events-more.png
var helpEventsMorePNG []byte

type helpCatalog struct {
	SchemaVersion int               `json:"schema_version"`
	Cards         []helpCatalogCard `json:"cards"`
}

type helpCatalogCard struct {
	ID      string             `json:"id"`
	Title   string             `json:"title"`
	Asset   string             `json:"asset"`
	SHA256  string             `json:"sha256"`
	Entries []helpCatalogEntry `json:"entries"`
}

type helpCatalogEntry struct {
	Syntax      string `json:"syntax"`
	Description string `json:"description"`
}

type helpCard struct {
	id     string
	asset  string
	sha256 string
	data   []byte
}

type HelpCardProvider struct{}

var embeddedHelpAssets = map[string][]byte{
	"help-broadcast.png":         helpBroadcastPNG,
	"help-member-alarm-news.png": helpMemberAlarmNewsPNG,
	"help-events-more.png":       helpEventsMorePNG,
}

var approvedHelpCards = mustLoadHelpCards(helpCatalogJSON, embeddedHelpAssets)

func NewHelpCardProvider() *HelpCardProvider {
	return &HelpCardProvider{}
}

func (p *HelpCardProvider) HelpImages(ctx context.Context) ([][]byte, error) {
	if p == nil {
		return nil, errors.New("help card provider is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	images := make([][]byte, len(approvedHelpCards))
	for index, card := range approvedHelpCards {
		images[index] = bytes.Clone(card.data)
	}
	return images, nil
}

func mustLoadHelpCards(data []byte, assets map[string][]byte) []helpCard {
	var catalog helpCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic(fmt.Sprintf("parse help card catalog: %v", err))
	}
	if err := validateHelpCatalog(catalog, assets); err != nil {
		panic(err)
	}

	cards := make([]helpCard, 0, len(catalog.Cards))
	for _, source := range catalog.Cards {
		imageData := assets[source.Asset]
		cards = append(cards, helpCard{
			id:     source.ID,
			asset:  source.Asset,
			sha256: source.SHA256,
			data:   imageData,
		})
	}
	return cards
}

func validateHelpCatalog(catalog helpCatalog, assets map[string][]byte) error {
	if catalog.SchemaVersion != 1 {
		return fmt.Errorf("help card schema_version = %d, want 1", catalog.SchemaVersion)
	}
	if len(catalog.Cards) == 0 || len(catalog.Cards) != len(assets) {
		return fmt.Errorf("help catalog cards = %d, embedded assets = %d", len(catalog.Cards), len(assets))
	}
	return validateHelpCatalogCards(catalog.Cards, assets)
}

func validateHelpCatalogCards(cards []helpCatalogCard, assets map[string][]byte) error {
	seenIDs := make(map[string]struct{}, len(cards))
	seenAssets := make(map[string]struct{}, len(cards))
	for index := range cards {
		card := &cards[index]
		if err := validateHelpCatalogCard(index, card, assets); err != nil {
			return err
		}
		if err := recordHelpCatalogCard(card, seenIDs, seenAssets); err != nil {
			return err
		}
	}
	return nil
}

func validateHelpCatalogCard(index int, card *helpCatalogCard, assets map[string][]byte) error {
	if err := validateHelpCatalogCardMetadata(index, card); err != nil {
		return err
	}
	if err := validateHelpCatalogEntries(card); err != nil {
		return err
	}
	return validateHelpCatalogAsset(card, assets)
}

func validateHelpCatalogCardMetadata(index int, card *helpCatalogCard) error {
	if strings.TrimSpace(card.ID) == "" || strings.TrimSpace(card.Title) == "" || strings.TrimSpace(card.Asset) == "" || strings.TrimSpace(card.SHA256) == "" {
		return fmt.Errorf("help card %d metadata must not be blank", index)
	}
	return nil
}

func validateHelpCatalogEntries(card *helpCatalogCard) error {
	if len(card.Entries) == 0 {
		return fmt.Errorf("help card %q must include entries", card.ID)
	}
	for entryIndex, entry := range card.Entries {
		if strings.TrimSpace(entry.Syntax) == "" || strings.TrimSpace(entry.Description) == "" {
			return fmt.Errorf("help card %q entry %d must not be blank", card.ID, entryIndex)
		}
	}
	return nil
}

func validateHelpCatalogAsset(card *helpCatalogCard, assets map[string][]byte) error {
	imageData, ok := assets[card.Asset]
	if !ok {
		return fmt.Errorf("help card %q references unknown asset %q", card.ID, card.Asset)
	}
	sum := sha256.Sum256(imageData)
	if actual := hex.EncodeToString(sum[:]); actual != card.SHA256 {
		return fmt.Errorf("help card %q SHA-256 = %q, want approved %q", card.ID, actual, card.SHA256)
	}
	config, err := png.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return fmt.Errorf("decode help card %q PNG: %w", card.ID, err)
	}
	if config.Width != helpCardWidth || config.Height != helpCardHeight {
		return fmt.Errorf("help card %q dimensions = %dx%d, want %dx%d", card.ID, config.Width, config.Height, helpCardWidth, helpCardHeight)
	}
	return nil
}

func recordHelpCatalogCard(card *helpCatalogCard, seenIDs, seenAssets map[string]struct{}) error {
	if _, exists := seenIDs[card.ID]; exists {
		return fmt.Errorf("help card id %q is duplicated", card.ID)
	}
	if _, exists := seenAssets[card.Asset]; exists {
		return fmt.Errorf("help card asset %q is duplicated", card.Asset)
	}
	seenIDs[card.ID] = struct{}{}
	seenAssets[card.Asset] = struct{}{}
	return nil
}
