package sourceobservation

import (
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
	"time"
)

func TestScheduleCollaboTalentNamesNormalizeAndRejectOverflow(t *testing.T) {
	t.Parallel()
	scheduled := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	payload, err := MarshalPayloadV1(ScheduleSnapshotV1{
		GroupKey: "UC_TEST",
		Items: []ScheduleItemV1{{
			ExternalID:         "schedule-1",
			Title:              "title",
			ScheduledAt:        scheduled,
			CollaboTalentNames: []string{"  Guest A  ", "", "Guest B"},
		}},
		Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	prepared, err := PrepareEnvelope(newPaginatedEnvelope(t, KindSchedule, payload, CompletenessComplete))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	var snapshot ScheduleSnapshotV1
	if err := jsonv2.Unmarshal(prepared.Payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("items = %#v", snapshot.Items)
	}
	got := snapshot.Items[0].CollaboTalentNames
	if len(got) != 2 || got[0] != "Guest A" || got[1] != "Guest B" {
		t.Fatalf("collabo names = %#v", got)
	}

	tooMany := make([]string, MaxScheduleCollaboTalentNames+1)
	for i := range tooMany {
		tooMany[i] = "guest"
	}
	if _, err := PrepareEnvelope(newPaginatedEnvelope(t, KindSchedule, mustMarshalPayload(t, ScheduleSnapshotV1{
		GroupKey: "UC_TEST",
		Items: []ScheduleItemV1{{
			ExternalID: "schedule-2", Title: "title", ScheduledAt: scheduled, CollaboTalentNames: tooMany,
		}},
		Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST"},
	}), CompletenessComplete)); err == nil {
		t.Fatal("expected overflow rejection")
	}

	tooLong := strings.Repeat("a", MaxScheduleCollaboTalentNameBytes+1)
	if _, err := PrepareEnvelope(newPaginatedEnvelope(t, KindSchedule, mustMarshalPayload(t, ScheduleSnapshotV1{
		GroupKey: "UC_TEST",
		Items: []ScheduleItemV1{{
			ExternalID: "schedule-3", Title: "title", ScheduledAt: scheduled, CollaboTalentNames: []string{tooLong},
		}},
		Coverage: ScheduleCoverageV1{GroupKey: "UC_TEST"},
	}), CompletenessComplete)); err == nil {
		t.Fatal("expected oversize name rejection")
	}
}
