package youtubejs

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestPAG013PaginationValidateAndQuality(t *testing.T) {
	t.Parallel()

	var fixture struct {
		Valid []struct {
			Reason       TerminationReason     `json:"reason"`
			Exhausted    bool                  `json:"exhausted"`
			Continuity   string                `json:"continuity"`
			Completeness contract.Completeness `json:"completeness"`
		} `json:"valid"`
		Invalid []struct {
			Reason     TerminationReason `json:"reason"`
			Exhausted  bool              `json:"exhausted"`
			Continuity string            `json:"continuity"`
		} `json:"invalid"`
	}

	_, file, _, ok := runtime.Caller(0)

	if !ok {
		t.Fatal("locate protocol_conformance_test.go")
	}

	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "youtubejs", "testdata", "pagination-tuples.json"))
	if err != nil {
		t.Fatal(err)
	}

	if err := jsonv2.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}

	for _, item := range fixture.Valid {
		page := Pagination{
			PageCount:         1,
			Exhausted:         item.Exhausted,
			Continuity:        item.Continuity,
			TerminationReason: item.Reason,
		}

		completeness, continuity, err := page.Quality()
		if err != nil {
			t.Fatalf("Quality(%q): %v", item.Reason, err)
		}

		if completeness != item.Completeness || continuity != contract.Continuity(item.Continuity) {
			t.Fatalf("Quality(%q) = %q/%q", item.Reason, completeness, continuity)
		}
	}

	for _, item := range fixture.Invalid {
		page := Pagination{
			PageCount:         1,
			Exhausted:         item.Exhausted,
			Continuity:        item.Continuity,
			TerminationReason: item.Reason,
		}
		if err := page.Validate(); err == nil {
			t.Fatalf("Validate accepted impossible tuple: %#v", page)
		}
	}
}
