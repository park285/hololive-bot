package youtubejs

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

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
		field := field
		if field.Anonymous {
			collectJSONFields(field.Type, fields)
			continue
		}
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
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
