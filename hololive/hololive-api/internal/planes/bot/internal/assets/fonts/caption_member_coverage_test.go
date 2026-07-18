package fonts

import (
	"testing"
	"unicode"

	"github.com/kapu/hololive-dbtest"
)

func TestCaptionFaceCoversSeededMemberNames(t *testing.T) {
	pool := dbtest.NewPool(t)
	rows, err := pool.Query(t.Context(), `
		SELECT english_name,
		       COALESCE(japanese_name, ''),
		       COALESCE(korean_name, ''),
		       COALESCE(short_korean_name, '')
		FROM members`)
	if err != nil {
		t.Fatalf("query members: %v", err)
	}
	defer rows.Close()

	face, err := CaptionFaceSized(24)
	if err != nil {
		t.Fatalf("CaptionFaceSized: %v", err)
	}

	memberCount := 0
	for rows.Next() {
		var en, ja, ko, short string
		if err := rows.Scan(&en, &ja, &ko, &short); err != nil {
			t.Fatalf("scan member: %v", err)
		}
		memberCount++
		for _, name := range []string{en, ja, ko, short} {
			for _, r := range name {
				if unicode.IsControl(r) {
					continue
				}
				if _, ok := face.GlyphAdvance(r); !ok {
					t.Errorf("rune %q(%U) in member name %q: 결합 face에 글리프 없음(두부)", r, r, name)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if memberCount == 0 {
		t.Fatal("seeded members = 0; coverage assertion is vacuous")
	}
	t.Logf("checked %d seeded members", memberCount)
}
