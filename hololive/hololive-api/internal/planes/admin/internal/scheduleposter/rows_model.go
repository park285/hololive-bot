package scheduleposter

import (
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

const maxRowsResponseBytes = 512 << 10

type rowModel interface {
	extractRows(ctx context.Context, src source, prompt string) ([]extractedRow, usage, error)
	translateRows(ctx context.Context, prompt string) ([]translatedRow, usage, error)
}

type extractedRow struct {
	Date          string                `json:"date"`
	Start         *string               `json:"start"`
	End           *string               `json:"end"`
	TZ            *string               `json:"tz"`
	Status        posterstore.RowStatus `json:"status"`
	Text          string                `json:"text"`
	Badges        []string              `json:"badges"`
	Updates       []string              `json:"-"`
	Disagreements []string              `json:"-"`
}

type translatedRow struct {
	Index   int                `json:"index"`
	TitleKo string             `json:"title_ko"`
	WithKo  translatedItems    `json:"with_ko"`
	PlaceKo string             `json:"place_ko"`
	TagsKo  translatedItems    `json:"tags_ko"`
	NoteKo  string             `json:"note_ko"`
	Terms   []posterstore.Term `json:"terms"`
	DoubtKo string             `json:"doubt_ko"`
}

type translatedItems []string

func (items *translatedItems) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if dec.PeekKind() != jsontext.KindBeginArray {
		return errors.ErrUnsupported
	}

	if _, err := dec.ReadToken(); err != nil {
		return err
	}

	out := translatedItems{}

	for {
		token, err := dec.ReadToken()
		if err != nil {
			return err
		}

		kind := token.Kind()
		if kind == jsontext.KindEndArray {
			*items = out
			return nil
		}

		if kind != jsontext.KindString {
			return errors.New("translated list item must be a string")
		}

		item := token.String()
		if strings.TrimSpace(item) != "" || hasControl(item) {
			out = append(out, item)
		}
	}
}

func stringSchema(description string) *genai.Schema {
	return &genai.Schema{Type: genai.TypeString, Description: description}
}

func nullableStringSchema(description string) *genai.Schema {
	return &genai.Schema{Type: genai.TypeString, Nullable: new(true), Description: description}
}

func stringListSchema(description string) *genai.Schema {
	return &genai.Schema{Type: genai.TypeArray, Description: description, Items: &genai.Schema{Type: genai.TypeString}}
}

func extractSchema() *genai.Schema {
	fields := []string{"date", "start", "end", "tz", "status", "text", "badges"}

	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Dated schedule slots transcribed from a weekly schedule poster.",
		Required:    []string{"rows"},
		Properties: map[string]*genai.Schema{
			"rows": {
				Type:        genai.TypeArray,
				Description: "Schedule slots in chronological order.",
				Items: &genai.Schema{
					Type:             genai.TypeObject,
					Required:         fields,
					PropertyOrdering: fields,
					Properties: map[string]*genai.Schema{
						"date":  stringSchema("Day cell of the slot as YYYY-MM-DD inside the poster week."),
						"start": nullableStringSchema("Printed start time as 24-hour HH:MM in the zone given in tz, or null."),
						"end":   nullableStringSchema("Printed end time in the same format, or null."),
						"tz":    nullableStringSchema("Time zone of start as printed, or null when the poster states none."),
						"status": {Type: genai.TypeString, Enum: []string{
							string(posterstore.RowPlanned), string(posterstore.RowTentative), string(posterstore.RowCancelled), string(posterstore.RowOff),
						}},
						"text": stringSchema(
							"Everything written in the slot except date, weekday, and its times in every zone, copied as printed."),
						"badges": stringListSchema("Badges attached to the slot: the printed text, or the legend's text for an icon."),
					},
				},
			},
		},
	}
}

func translateSchema() *genai.Schema {
	fields := []string{"index", "title_ko", "with_ko", "place_ko", "tags_ko", "note_ko", "terms", "doubt_ko"}
	termFields := []string{"source", "ko", "basis"}

	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Korean card fields corresponding 1-to-1 with the numbered input schedule rows.",
		Required:    []string{"rows"},
		Properties: map[string]*genai.Schema{
			"rows": {
				Type:        genai.TypeArray,
				Description: "One item per input row, in input order.",
				Items: &genai.Schema{
					Type:             genai.TypeObject,
					Required:         fields,
					PropertyOrdering: fields,
					Properties: map[string]*genai.Schema{
						"index":    {Type: genai.TypeInteger, Description: "1-based index matching the input row number."},
						"title_ko": stringSchema("What the stream is, without participants, place, labels, or remarks."),
						"with_ko":  stringListSchema("Participants besides the poster owner."),
						"place_ko": stringSchema(
							"Where the stream airs or the event happens when it is not the poster owner's own channel; otherwise empty."),
						"tags_ko": stringListSchema("Fixed properties: stream language, platform, restricted access, sponsorship."),
						"note_ko": stringSchema("Remarks about the plan; otherwise empty."),
						"terms": {
							Type: genai.TypeArray,
							Description: "Names and stream jargon rendered into Korean without a Glossary, Earlier renderings, " +
								"Name table, or standing vocabulary entry.",
							Items: &genai.Schema{
								Type:             genai.TypeObject,
								Required:         termFields,
								PropertyOrdering: termFields,
								Properties: map[string]*genai.Schema{
									"source": stringSchema("The name as written in the row."),
									"ko":     stringSchema("The Korean written for it."),
									"basis":  {Type: genai.TypeString, Enum: termBases},
								},
							},
						},
						"doubt_ko": stringSchema("Open points the row left undetermined; otherwise empty."),
					},
				},
			},
		},
	}
}

func rowsRequestConfig(stage RowStageConfig, system string, schema *genai.Schema) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		HTTPOptions:       &genai.HTTPOptions{Timeout: new(stage.timeout())},
		SystemInstruction: &genai.Content{Parts: []*genai.Part{genai.NewPartFromText(system)}},
		Temperature:       stage.Temperature,
		CandidateCount:    1,
		ResponseMIMEType:  "application/json",
		ResponseSchema:    schema,
		ThinkingConfig:    &genai.ThinkingConfig{ThinkingLevel: stage.ThinkingLevel},
	}
}

func extractRequest(stage RowStageConfig, src source, prompt string) ([]*genai.Content, *genai.GenerateContentConfig) {
	return posterContents(src, prompt), rowsRequestConfig(stage, extractSystemInstruction, extractSchema())
}

func translateRequest(stage RowStageConfig, prompt string) ([]*genai.Content, *genai.GenerateContentConfig) {
	return []*genai.Content{genai.NewContentFromParts([]*genai.Part{genai.NewPartFromText(prompt)}, genai.RoleUser)},
		rowsRequestConfig(stage, translateSystemInstruction, translateSchema())
}

func (t *vertexTranslator) extractRows(ctx context.Context, src source, prompt string) ([]extractedRow, usage, error) {
	contents, config := extractRequest(t.rows.Extract, src, prompt)

	response, err := generateContent(ctx, t.client.Models, t.rows.Extract.Model, contents, config)
	if err != nil {
		return nil, usage{}, fmt.Errorf("extract poster rows: %w", err)
	}

	rows, err := decodeRowsResponse[extractedRow](response)

	return rows, responseUsage(t.rows.Extract.Model, response), err
}

func (t *vertexTranslator) translateRows(ctx context.Context, prompt string) ([]translatedRow, usage, error) {
	contents, config := translateRequest(t.rows.Translate, prompt)

	response, err := generateContent(ctx, t.client.Models, t.rows.Translate.Model, contents, config)
	if err != nil {
		return nil, usage{}, fmt.Errorf("translate poster rows: %w", err)
	}

	rows, err := decodeRowsResponse[translatedRow](response)

	return rows, responseUsage(t.rows.Translate.Model, response), err
}

func decodeRowsResponse[T any](response *genai.GenerateContentResponse) ([]T, error) {
	if response == nil {
		return nil, errors.New("poster rows response is empty")
	}

	if len(response.Candidates) == 0 || response.Candidates[0] == nil || response.Candidates[0].Content == nil {
		return nil, fmt.Errorf("poster rows response has no candidate (%s)", emptyResponseReason(response))
	}

	return decodeRows[T](response.Text())
}

func decodeRows[T any](text string) ([]T, error) {
	if len(text) > maxRowsResponseBytes {
		return nil, fmt.Errorf("poster rows response exceeds %d bytes", maxRowsResponseBytes)
	}

	var decoded struct {
		Rows *[]T `json:"rows"`
	}

	if err := jsonv2.Unmarshal([]byte(text), &decoded, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, fmt.Errorf("decode poster rows response: %s", clip(err.Error()))
	}

	if decoded.Rows == nil {
		return nil, errors.New("poster rows response has no rows array")
	}

	rows := *decoded.Rows
	if len(rows) > posterstore.MaxRows {
		return nil, fmt.Errorf("poster rows response has %d rows, more than %d", len(rows), posterstore.MaxRows)
	}

	return rows, nil
}
