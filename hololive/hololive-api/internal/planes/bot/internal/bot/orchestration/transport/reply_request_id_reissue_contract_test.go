package transport

import (
	"strings"
	"testing"

	iris "github.com/park285/iris-client-go/v2/iris"
)

func TestReissuedReplyClientRequestIDRejectsOutOfRangeAndNestedBases(t *testing.T) {
	t.Parallel()

	base := replyClientRequestID("message-1", 0)
	if base == "" {
		t.Fatal("replyClientRequestID() returned empty base")
	}

	if got := reissuedReplyClientRequestID(base, iris.ReplyReissueMaxGenerations+1); got != "" {
		t.Fatalf("out-of-range reissue = %q, want empty fail-closed result", got)
	}

	first := reissuedReplyClientRequestID(base, 1)
	if first == "" || !strings.HasSuffix(first, ":r1") {
		t.Fatalf("first reissue = %q, want :r1 suffix", first)
	}

	if got := reissuedReplyClientRequestID(first, 2); got != "" {
		t.Fatalf("nested reissue = %q, want empty fail-closed result", got)
	}
}
