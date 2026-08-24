package holodexcollector

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestParseLiveRowsSkipsUnsupportedStatusRows(t *testing.T) {
	t.Parallel()

	body := []byte(`[
		{"id":"video-a","status":"live","channel_id":"UC_A"},
		{"id":"video-b","status":"missing","channel_id":"UC_A"},
		{"id":"video-c","status":"new","channel_id":"UC_B"},
		{"id":"video-d","status":"upcoming","channel_id":"UC_B"}
	]`)

	rows, err := parseLiveRows(body)
	if err != nil {
		t.Fatal(err)
	}

	if got := parsedIDs(rows); len(got) != 2 || got[0] != "video-a" || got[1] != "video-d" {
		t.Fatalf("ids = %#v", got)
	}
}

func TestParseLiveRowsSkipsMalformedRowsAndDeduplicates(t *testing.T) {
	t.Parallel()

	body := []byte(`[
		{"id":"video-a","status":"live","channel_id":"UC_A","live_viewers":5},
		"not-an-object",
		{"id":"   ","status":"live","channel_id":"UC_A"},
		{"id":"video-conflict","status":"live","channel_id":"UC_A","channel":{"id":"UC_B"}},
		{"id":"video-nochannel","status":"live"},
		{"id":"video-badtime","status":"live","channel_id":"UC_A","start_scheduled":"14/08/2026 10:00"},
		{"id":"video-negviewers","status":"live","channel_id":"UC_A","live_viewers":-1},
		{"id":"video-strviewers","status":"live","channel_id":"UC_A","live_viewers":"hidden"},
		{"id":"video-a","status":"upcoming","channel_id":"UC_A"},
		{"id":"video-b","status":"past","channel_id":"UC_B"}
	]`)

	rows, err := parseLiveRows(body)
	if err != nil {
		t.Fatal(err)
	}

	got := parsedIDs(rows)
	if len(got) != 2 || got[0] != "video-a" || got[1] != "video-b" {
		t.Fatalf("ids = %#v", got)
	}

	if rows[0].status != "LIVE" {
		t.Fatalf("first surviving row lost its original status: %#v", rows[0].status)
	}
}

func TestParseLiveRowsFailsWhenEveryRowIsInvalid(t *testing.T) {
	t.Parallel()

	body := []byte(`[
		{"id":"video-a","status":"missing","channel_id":"UC_A"},
		{"id":"video-b","status":"new","channel_id":"UC_B"}
	]`)

	rows, err := parseLiveRows(body)
	if err == nil {
		t.Fatalf("fully invalid response parsed as %#v", rows)
	}

	if collecterr.CodeOf(err) != collecterr.ParserDrift || collecterr.ClassOf(err) != collecterr.ClassDataContract {
		t.Fatalf("code=%q class=%q err=%v", collecterr.CodeOf(err), collecterr.ClassOf(err), err)
	}

	if rows != nil {
		t.Fatalf("rows returned alongside error: %#v", rows)
	}
}

func TestParseLiveRowsFailsOnBrokenResponse(t *testing.T) {
	t.Parallel()

	tests := map[string][]byte{
		"object":       []byte(`{"id":"video-a"}`),
		"invalid json": []byte(`[{"id":`),
		"empty body":   nil,
		"json null":    []byte(`null`),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rows, err := parseLiveRows(body)
			if err == nil {
				t.Fatalf("broken response parsed as %#v", rows)
			}

			if collecterr.CodeOf(err) != collecterr.ParserDrift {
				t.Fatalf("code = %q, err = %v", collecterr.CodeOf(err), err)
			}
		})
	}
}

func TestParseLiveRowsAcceptsEmptyArray(t *testing.T) {
	t.Parallel()

	rows, err := parseLiveRows([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 0 {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestLiveRunnerKeepsHealthyRowsWhenOneRowIsUnsupported(t *testing.T) {
	t.Parallel()

	var rows []jsontext.Value

	if err := jsonv2.Unmarshal(testdata(t, "live.json"), &rows); err != nil {
		t.Fatal(err)
	}

	privated := jsontext.Value(`{
		"id":"vidGone04","title":"Privated","channel_id":"UC_A","status":"missing",
		"start_scheduled":"2026-08-14T11:00:00Z","channel":{"id":"UC_A","photo":"https://img.test/a.jpg","name":"A"}
	}`)

	body, err := jsonv2.Marshal(append([]jsontext.Value{privated}, rows...))
	if err != nil {
		t.Fatal(err)
	}

	output, err := NewLiveRunner(&staticFetcher{body: body}).Collect(t.Context(), holodexInput(t, []string{channelA, channelB}))
	if err != nil {
		t.Fatalf("one privated row aborted the whole batch: %v", err)
	}

	observations := output.Output().Observations()
	snapshots := 0

	for i := range observations {
		envelope := &observations[i]
		if envelope.SubjectKey == "vidGone04" {
			t.Fatalf("skipped row was published as %s", envelope.ObservationKind)
		}

		if envelope.ObservationKind == contract.KindLiveSnapshot {
			snapshots++
		}
	}

	if snapshots != 2 {
		t.Fatalf("live snapshots = %d, want UC_A and UC_B", snapshots)
	}
}

func parsedIDs(rows []parsedLive) []string {
	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].row.ID)
	}

	return ids
}
