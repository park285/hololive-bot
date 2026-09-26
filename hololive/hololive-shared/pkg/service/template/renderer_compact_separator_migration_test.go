package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	fieldStatus               = "Status"
	fieldTypesLabel           = "TypesLabel"
	fieldPrefix               = "Prefix"
	compactSeparatorMigration = "217_template_compact_separators.sql"
	wideItemSeparator         = "\n\n──────────\n\n"
	compactItemSeparator      = "\n──────────\n"
)

func loadCompactSeparatorSeeds(tb testing.TB) map[domain.TemplateKey]displayLineSeedPair {
	tb.Helper()

	dir := filepath.Join("..", "..", "..", "..", "hololive-api", "scripts", "migrations")

	raw, err := fs.ReadFile(os.DirFS(dir), compactSeparatorMigration)
	if err != nil {
		tb.Fatal(err)
	}

	rows := regexp.MustCompile(`(?s)\('([^']+)', \$old\$(.*?)\$old\$, \$new\$(.*?)\$new\$\)`).FindAllStringSubmatch(string(raw), -1)
	if len(rows) != 15 {
		tb.Fatalf("expected 15 updated template keys, got %d", len(rows))
	}

	pairs := make(map[domain.TemplateKey]displayLineSeedPair, len(rows))
	for _, row := range rows {
		pairs[domain.TemplateKey(row[1])] = displayLineSeedPair{row[2], row[3]}
	}

	return pairs
}

// 구분선 간격만 줄이고 항목 내용·순서·머리 문단은 그대로여야 한다.
func TestCompactSeparatorMigrationOnlyNarrowsItemGaps(t *testing.T) {
	pool := dbtest.NewPool(t)
	displayLine := loadDisplayLineSeeds(t)

	for key, pair := range loadCompactSeparatorSeeds(t) {
		t.Run(string(key), func(t *testing.T) {
			if previous, ok := displayLine[key]; !ok || previous.newBody != pair.oldBody {
				t.Fatal("217 must start from the 208 standard body")
			}

			if seedBody(t, pool, key) != pair.newBody {
				t.Fatal("standard default was not migrated")
			}

			if key == domain.TemplateKeyCmdAlarmList {
				return
			}

			data := repeatSampleItems(sampledata.GetTemplateSampleData(key))
			before := renderOptimizationTemplate(t, pair.oldBody, templateFuncs, data)
			after := renderOptimizationTemplate(t, pair.newBody, templateFuncs, data)

			if !strings.Contains(before, wideItemSeparator) {
				t.Fatalf("sample data did not exercise the item separator: %q", before)
			}

			if want := strings.ReplaceAll(before, wideItemSeparator, compactItemSeparator); after != want {
				t.Errorf("layout changed beyond separator gaps: got=%q want=%q", after, want)
			}
		})
	}
}

func TestCompactAlarmListKeepsSimpleAlarmsOnOneLine(t *testing.T) {
	pool := dbtest.NewPool(t)
	body := seedBody(t, pool, domain.TemplateKeyCmdAlarmList)

	got := renderOptimizationTemplate(t, body, templateFuncs, compactAlarmList(6, []map[string]any{
		compactAlarm("미오", "", nil),
		compactAlarm("비비", "방송+쇼츠", nil),
		compactAlarm("이로하", "", compactNextStream("upcoming", "마인크래프트", "https://youtu.be/upcoming123", "22:00", "2시간 후")),
		compactAlarm("스이세이", "", compactNextStream("ended", "지난 방송", "https://youtu.be/ended123", "", "")),
		compactAlarm("라덴", "", compactNextStream("live", "노래", "https://youtu.be/live123", "", "")),
		compactAlarm("리글로스", "", nil),
	}))

	want := "🔔 설정된 알람 · 6개\n\n" +
		"1 · 미오\n" +
		"2 · 비비 (방송+쇼츠)\n" +
		"──────────\n" +
		"3 · 이로하\n⏰ 22:00 (2시간 후)\n\u200b마인크래프트\nhttps://youtu.be/upcoming123\n" +
		"──────────\n" +
		"4 · 스이세이\n" +
		"──────────\n" +
		"5 · 라덴\n🔴 방송 중\n\u200b노래\nhttps://youtu.be/live123\n" +
		"──────────\n" +
		"6 · 리글로스"
	if got != want {
		t.Fatalf("compact alarm list mismatch:\ngot =%q\nwant=%q", got, want)
	}

	empty := renderOptimizationTemplate(t, body, templateFuncs, compactAlarmList(0, []map[string]any{}))
	if empty != "🔔 설정된 알람이 없습니다.\n예) !알람 추가 페코라" {
		t.Fatalf("empty alarm list changed: %q", empty)
	}
}

func compactNextStream(status, title, url, scheduled, detail string) map[string]any {
	return map[string]any{
		fieldStatus: status, fieldTitle: title, fieldURL: url,
		fieldScheduledKST: scheduled, "TimeDetail": detail, "StartingSoon": false,
	}
}

func compactAlarm(name, types string, next map[string]any) map[string]any {
	entry := map[string]any{fieldMemberName: name, fieldTypesLabel: types, fieldNextStream: nil}

	if next != nil {
		entry[fieldNextStream] = next
	}

	return entry
}

func compactAlarmList(count int, alarms []map[string]any) map[string]any {
	return map[string]any{fieldCount: count, fieldPrefix: "!", "Alarms": alarms}
}

// 샘플의 단일 항목 목록을 두 번 반복해 항목 사이 구분선을 실제로 렌더한다.
func repeatSampleItems(data any) any {
	root, ok := data.(map[string]any)
	if !ok {
		return data
	}

	out := make(map[string]any, len(root))

	for field, value := range root {
		items := reflect.ValueOf(value)
		if items.Kind() != reflect.Slice || items.Len() == 0 {
			out[field] = value

			continue
		}

		out[field] = reflect.AppendSlice(items, items).Interface()
	}

	return out
}
