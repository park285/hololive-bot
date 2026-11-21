package eventgrade

import "time"

const (
	SchemaVersion  = 1
	BoardKindScore = "marathon_score"
	KindCatalog    = "catalog"
	KindCutoff     = "cutoff"
)

type Chapter struct {
	EventID     string    `json:"event_id"`
	ChapterID   string    `json:"chapter_id"`
	EventType   string    `json:"event_type"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	EventEndsAt time.Time `json:"event_ends_at"`
	RevealsAt   time.Time `json:"reveals_at"`
}

type Binding struct {
	EventID   string `json:"event_id"`
	ChapterID string `json:"chapter_id"`
}

type Rank struct {
	Rank  int32  `json:"rank"`
	Score *int64 `json:"score,string"`
}

type CatalogObservation struct {
	Chapters          []Chapter `json:"chapters"`
	Bindings          []Binding `json:"bindings"`
	Conflicts         []string  `json:"conflicts"`
	QuarantineMissing bool      `json:"quarantine_missing"`
}

type CutoffObservation struct {
	EventID   string `json:"event_id"`
	ChapterID string `json:"chapter_id"`
	BoardKind string `json:"board_kind"`
	Final     *bool  `json:"final"`
	Ranks     []Rank `json:"ranks"`
}
