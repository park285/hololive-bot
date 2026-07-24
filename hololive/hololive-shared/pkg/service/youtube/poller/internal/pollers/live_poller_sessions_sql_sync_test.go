package pollers

import (
	"regexp"
	"strings"
	"testing"
)

// skip 가드(WHERE)는 SET 병합식의 손동기화 복제라, SET에만 컬럼을 추가하면 2분 창 안의
// 해당 컬럼 변경이 에러 없이 스킵된다. 두 블록의 excluded.* 집합 일치가 그 회귀를 막는다.
func TestLivePollerSessionsUpsertGuardCoversAllMergedColumns(t *testing.T) {
	query := mustSQL("live_poller_sessions_0044_01.sql")

	setStart := strings.Index(query, "DO UPDATE SET")
	if setStart < 0 {
		t.Fatal("upsert query must contain DO UPDATE SET")
	}
	afterSet := query[setStart:]
	whereStart := strings.Index(afterSet, "WHERE")
	if whereStart < 0 {
		t.Fatal("upsert query must contain a skip-guard WHERE clause")
	}

	pattern := regexp.MustCompile(`excluded\.([a-z_]+)`)
	collect := func(section string) map[string]bool {
		columns := make(map[string]bool)
		for _, match := range pattern.FindAllStringSubmatch(section, -1) {
			columns[match[1]] = true
		}
		return columns
	}

	setColumns := collect(afterSet[:whereStart])
	guardColumns := collect(afterSet[whereStart:])

	for column := range setColumns {
		if !guardColumns[column] {
			t.Errorf("column %q is merged in SET but missing from the skip guard WHERE", column)
		}
	}
	for column := range guardColumns {
		if !setColumns[column] {
			t.Errorf("column %q is referenced in the skip guard WHERE but not merged in SET", column)
		}
	}
}
