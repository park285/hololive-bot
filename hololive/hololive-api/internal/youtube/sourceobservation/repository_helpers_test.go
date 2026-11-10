package sourceobservation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedErrorDetailPreservesUTF8(t *testing.T) {
	t.Parallel()

	// 3바이트 문자 683개는 2049바이트라 2048바이트 상한이 문자 중간에 걸린다.
	got := boundedErrorDetail(strings.Repeat("가", 683))
	if want := strings.Repeat("가", 682); got != want || !utf8.ValidString(got) {
		t.Fatalf("bounded error detail = %d bytes (valid UTF-8 %t), want %d bytes", len(got), utf8.ValidString(got), len(want))
	}
}
