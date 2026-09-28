package template

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const nextStreamRemovalMigration = "233_alarm_next_stream_template_removal.sql"

func loadNextStreamRemovalSeeds(tb testing.TB) map[domain.TemplateKey]displayLineSeedPair {
	tb.Helper()

	dir := filepath.Join("..", "..", "..", "..", "hololive-api", "scripts", "migrations")

	raw, err := fs.ReadFile(os.DirFS(dir), nextStreamRemovalMigration)
	if err != nil {
		tb.Fatal(err)
	}

	rows := regexp.MustCompile(`(?s)\('([^']+)', \$old\$(.*?)\$old\$, \$new\$(.*?)\$new\$\)`).FindAllStringSubmatch(string(raw), -1)

	pairs := make(map[domain.TemplateKey]displayLineSeedPair, len(rows))
	for _, row := range rows {
		pairs[domain.TemplateKey(row[1])] = displayLineSeedPair{row[2], row[3]}
	}

	return pairs
}

// 운영에서 NextStream은 한 번도 채워진 적이 없으므로, 새 본문은 NextStream 없이 렌더되고
// 이전 본문이 NextStream=nil로 내던 출력과 바이트 단위로 같아야 한다.
func TestNextStreamRemovalMigrationKeepsOutputWithoutNextStream(t *testing.T) {
	pairs := loadNextStreamRemovalSeeds(t)

	cases := map[domain.TemplateKey][]map[string]any{
		domain.TemplateKeyCmdAlarmAdded: {
			{"Added": true, fieldMemberName: "미코 **Miko**"},
			{"Added": false, fieldMemberName: "미코"},
		},
		domain.TemplateKeyCmdAlarmList: {
			compactAlarmList(0, []map[string]any{}),
			compactAlarmList(1, []map[string]any{compactAlarm("미오", "")}),
			compactAlarmList(3, []map[string]any{compactAlarm("미오", ""), compactAlarm("비비", "방송+쇼츠"), compactAlarm("리글로스 _x_", "")}),
		},
	}

	if len(pairs) != len(cases) {
		t.Fatalf("migration pairs=%d want %d", len(pairs), len(cases))
	}

	for key, inputs := range cases {
		pair, ok := pairs[key]
		if !ok {
			t.Fatalf("migration does not rewrite %s", key)
		}

		for _, data := range inputs {
			after := renderOptimizationTemplate(t, pair.newBody, templateFuncs, data)
			before := renderOptimizationTemplate(t, pair.oldBody, templateFuncs, withNilNextStream(data))

			if after != before {
				t.Errorf("%s output changed:\nold=%q\nnew=%q", key, before, after)
			}
		}
	}
}

// 이전 본문은 NextStream 키를 요구하므로 비어 있는 값을 넣어 운영 입력을 재현한다.
func withNilNextStream(data map[string]any) map[string]any {
	out := make(map[string]any, len(data)+1)
	maps.Copy(out, data)

	out[fieldNextStream] = nil

	alarms, ok := data["Alarms"].([]map[string]any)
	if !ok {
		return out
	}

	withNil := make([]map[string]any, 0, len(alarms))
	for _, alarm := range alarms {
		entry := make(map[string]any, len(alarm)+1)
		maps.Copy(entry, alarm)

		entry[fieldNextStream] = nil
		withNil = append(withNil, entry)
	}

	out["Alarms"] = withNil

	return out
}
