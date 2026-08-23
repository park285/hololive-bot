package youtubejs

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func TestGoProtocolJSONTagsMatchContractsDTS(t *testing.T) {
	t.Parallel()
	dts := readContractsDTS(t)
	cases := []struct {
		name  string
		value any
	}{
		{"Pagination", Pagination{}},
		{"BootstrapProxy", BootstrapProxy{}},
		{"BootstrapLimits", BootstrapLimits{}},
		{"BootstrapRequest", BootstrapRequest{}},
		{"BootstrapResponse", BootstrapResponse{}},
		{"HealthResponse", HealthResponse{}},
		{"CommunityRequest", CommunityRequest{}},
		{"ContentRequest", ContentRequest{}},
		{"ChannelRequest", ChannelRequest{}},
		{"ViewerRequest", ViewerRequest{}},
		{"CommunityResult", CommunityResult{}},
		{"ContentItem", ContentItem{}},
		{"ContentResult", ContentResult{}},
		{"LiveSessionItem", LiveSessionItem{}},
		{"ChannelStatsItem", ChannelStatsItem{}},
		{"ChannelProfileItem", ChannelProfileItem{}},
		{"ChannelPhotoVariant", ChannelPhotoVariant{}},
		{"ChannelResult", ChannelResult{}},
		{"ViewerResult", ViewerResult{}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			goFields := jsonFieldSet(reflect.TypeOf(test.value))
			dtsFields := dtsInterfaceFields(dts, test.name)
			if len(dtsFields) == 0 {
				t.Fatalf("contracts.d.ts missing interface %s", test.name)
			}
			for field := range goFields {
				if !dtsFields[field] {
					t.Fatalf("Go json tag %q is missing from contracts.d.ts %s", field, test.name)
				}
			}
			for field := range dtsFields {
				if !goFields[field] {
					t.Fatalf("contracts.d.ts field %q is missing from Go %s", field, test.name)
				}
			}
		})
	}
}

func TestProtocolTypesStayOnTheSharedWire(t *testing.T) {
	t.Parallel()
	var _ *parser.CommunityPost
	var _ *time.Time
}

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

func TestPAG012PaginationCursorJSONByteBound(t *testing.T) {
	t.Parallel()
	accepted := Pagination{
		PageCount:         1,
		CursorStart:       strings.Repeat("x", 8190),
		Exhausted:         false,
		Continuity:        "GAP_UNRESOLVED",
		TerminationReason: TerminationMaxPages,
	}
	if encoded, err := jsonv2.Marshal(accepted.CursorStart); err != nil || len(encoded) != 8192 {
		t.Fatalf("accepted cursor bytes = %d, error = %v", len(encoded), err)
	}
	if err := accepted.Validate(); err != nil {
		t.Fatalf("Validate accepted cursor: %v", err)
	}
	accepted.CursorStart += "x"
	if err := accepted.Validate(); err == nil {
		t.Fatal("Validate accepted 8193-byte cursor")
	}
}

func readContractsDTS(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate protocol_conformance_test.go")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "youtubejs", "src", "contracts.d.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonFieldSet(typ reflect.Type) map[string]bool {
	fields := make(map[string]bool)
	collectJSONFields(typ, fields)
	return fields
}

func collectJSONFields(typ reflect.Type, fields map[string]bool) {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for field := range typ.Fields() {
		if field.Anonymous {
			collectJSONFields(field.Type, fields)
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" {
			fields[name] = true
		}
	}
}

func dtsInterfaceFields(src, name string) map[string]bool {
	fields := make(map[string]bool)
	collectDTSInterface(src, name, fields, map[string]bool{})
	return fields
}

func collectDTSInterface(src, name string, fields, seen map[string]bool) {
	if seen[name] {
		return
	}
	seen[name] = true
	header := regexp.MustCompile(`export interface ` + regexp.QuoteMeta(name) + `(?:\s+extends\s+([A-Za-z0-9_]+))?\s*\{`)
	loc := header.FindStringSubmatchIndex(src)
	if loc == nil {
		return
	}
	if loc[2] >= 0 {
		collectDTSInterface(src, src[loc[2]:loc[3]], fields, seen)
	}
	body := src[loc[1]:]
	before, _, ok := strings.Cut(body, "\n}")
	if !ok {
		return
	}
	fieldLine := regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\??:`)
	for _, match := range fieldLine.FindAllStringSubmatch(before, -1) {
		fields[match[1]] = true
	}
}
