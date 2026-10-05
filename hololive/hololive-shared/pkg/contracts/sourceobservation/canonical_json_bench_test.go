package sourceobservation

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkCanonicalizeJSON1MiB(b *testing.B) {
	n := MaxPayloadBytes - 8
	raw := []byte(`{"v":"` + strings.Repeat("a", n) + `"}`)

	if len(raw) > MaxPayloadBytes {
		raw = raw[:MaxPayloadBytes]
	}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := CanonicalizeJSON(raw); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCanonicalizeJSONVideoList는 정렬되지 않은 key, escape 문자열, 지수 숫자를 가진
// 다수의 object 배열이라는 실제 수집 payload 형태를 측정한다.
func BenchmarkCanonicalizeJSONVideoList(b *testing.B) {
	raw := benchmarkVideoListJSON(b)

	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()

	for b.Loop() {
		if _, err := CanonicalizeJSON(raw); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeStrictJSONVideoList(b *testing.B) {
	raw := benchmarkVideoListJSON(b)

	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()

	for b.Loop() {
		var decoded any

		if err := decodeStrictJSON(raw, &decoded); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkVideoListJSON(b *testing.B) []byte {
	b.Helper()

	var builder strings.Builder

	builder.WriteString(`{"videos":[`)

	for index := range 2000 {
		if index > 0 {
			builder.WriteByte(',')
		}

		fmt.Fprintf(
			&builder,
			`{"video_id":"vid%07d","title":"방송 \"%d\" caf\u00e9\n","published_at":"2026-10-05T12:%02d:00Z",`+
				`"view_count":%de1,"is_short":false,"tags":["hololive","live",null],`+
				`"channel":{"name":"Channel %d","channel_id":"UC%022d"}}`,
			index, index, index%60, index, index%50, index,
		)
	}

	builder.WriteString(`],"exhausted":true,"channel_id":"UC_TEST","coverage":{"page_count":40,"max_results":50}}`)

	if builder.Len() > MaxPayloadBytes {
		b.Fatalf("benchmark payload is %d bytes, exceeds %d", builder.Len(), MaxPayloadBytes)
	}

	return []byte(builder.String())
}
