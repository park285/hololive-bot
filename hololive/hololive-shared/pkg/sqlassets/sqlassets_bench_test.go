package sqlassets

import (
	"strings"
	"testing"
	"testing/fstest"
)

// BenchmarkMustReader는 렌더(renderer_resolve.sql, 217B)와 배치 렌더
// (renderer_resolve_batch.sql, 557B) 크기의 자산을 같은 이름으로 반복 읽는
// 운영 호출 형태를 측정한다.
func BenchmarkMustReader(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{
		{name: "217B", bytes: 217},
		{name: "557B", bytes: 557},
	} {
		b.Run(size.name, func(b *testing.B) {
			query := "SELECT " + strings.Repeat("x", size.bytes-len("SELECT ;\n")) + ";\n"
			read := MustReader(
				fstest.MapFS{
					"queries/query.sql": &fstest.MapFile{Data: []byte(query)},
				},
				"queries",
			)

			b.ReportAllocs()

			for b.Loop() {
				if got := read("query.sql"); len(got) != size.bytes {
					b.Fatalf("len(read(query.sql)) = %d, want %d", len(got), size.bytes)
				}
			}
		})
	}
}
