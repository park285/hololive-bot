package officialcollector

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestParseScheduleRowCollectsCollaboTalentNames(t *testing.T) {
	t.Parallel()

	body := []byte(`{"dateGroupList":[{"videoList":[{
		"datetime":"2026/08/14 20:00:00",
		"isLive":false,
		"url":"https://www.youtube.com/watch?v=collabrow001",
		"title":"Collab",
		"name":"Host Talent",
		"talent":{"name":"Host Talent"},
		"collaboTalents":[
			{"name":"Host Talent"},
			{"name":"  Guest One  "},
			{"name":""},
			{"name":"Guest Two"},
			{"name":"guest two"}
		]
	}]}]}`)

	items, err := parseScheduleItems(body)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}

	got := items[0].CollaboTalentNames
	if len(got) != 2 || got[0] != "Guest One" || got[1] != "Guest Two" {
		t.Fatalf("collabo names = %#v", got)
	}
}

func TestParseScheduleRowRejectsInvalidCollaboTalentsType(t *testing.T) {
	t.Parallel()

	rawRow := jsontext.Value(`{
		"datetime":"2026/08/14 20:00:00",
		"url":"https://www.youtube.com/watch?v=collabrow002",
		"title":"Talk",
		"name":"Host Talent",
		"collaboTalents":{"name":"not-an-array"}
	}`)
	if _, err := parseScheduleRow(rawRow); err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestParseScheduleRowRejectsCollaboTalentNamesOverflow(t *testing.T) {
	t.Parallel()

	talents := make([]map[string]string, 0, contract.MaxScheduleCollaboTalentNames+2)
	for i := range contract.MaxScheduleCollaboTalentNames + 2 {
		talents = append(talents, map[string]string{"name": "Guest " + strings.Repeat("x", i+1)})
	}

	row := map[string]any{
		"datetime":       "2026/08/14 20:00:00",
		"url":            "https://www.youtube.com/watch?v=collabrow003",
		"title":          "Talk",
		"name":           "Host Talent",
		"collaboTalents": talents,
	}

	rawRow, err := jsonv2.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := parseScheduleRow(rawRow); err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestParseScheduleRowRejectsOversizeCollaboTalentName(t *testing.T) {
	t.Parallel()

	row := map[string]any{
		"datetime": "2026/08/14 20:00:00",
		"url":      "https://www.youtube.com/watch?v=collabrow005",
		"title":    "Talk",
		"name":     "Host Talent",
		"collaboTalents": []map[string]string{{
			"name": strings.Repeat("x", contract.MaxScheduleCollaboTalentNameBytes+1),
		}},
	}

	rawRow, err := jsonv2.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := parseScheduleRow(rawRow); err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift {
		t.Fatalf("error = %v", err)
	}
}

func TestRunnerPublishesCollaboTalentNames(t *testing.T) {
	t.Parallel()

	body := []byte(`{"dateGroupList":[{"videoList":[{
		"datetime":"2026/08/14 20:00:00",
		"url":"https://www.youtube.com/watch?v=collabrow004",
		"title":"Collab",
		"name":"Host Talent",
		"collaboTalents":[{"name":"Guest One"}]
	}]}]}`)
	observation := mustSingleObservation(t, mustCollect(t, body))

	var payload contract.ScheduleSnapshotV1

	if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	if len(payload.Items) != 1 {
		t.Fatalf("items = %#v", payload.Items)
	}

	got := payload.Items[0].CollaboTalentNames
	if len(got) != 1 || got[0] != "Guest One" {
		t.Fatalf("collabo names = %#v", got)
	}

	if payload.Items[0].ScheduledAt.IsZero() || payload.Items[0].ScheduledAt.Location() != time.UTC {
		t.Fatalf("scheduled_at = %v", payload.Items[0].ScheduledAt)
	}
}
