package transport

import (
	"errors"
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

	if got, err := reissuedReplyClientRequestID(base, iris.ReplyReissueMaxGenerations+1); err == nil {
		t.Fatalf("out-of-range reissue = %q, want fail-closed error", got)
	} else if !errors.Is(err, iris.ErrReplyReissueGenerationOutOfRange) {
		t.Fatalf("out-of-range reissue error = %v, want ErrReplyReissueGenerationOutOfRange", err)
	}

	first, err := reissuedReplyClientRequestID(base, 1)
	if err != nil || !strings.HasSuffix(first, ":r1") {
		t.Fatalf("first reissue = %q, %v, want :r1 suffix", first, err)
	}

	if got, err := reissuedReplyClientRequestID(first, 2); err == nil {
		t.Fatalf("nested reissue = %q, want fail-closed error", got)
	} else if !errors.Is(err, iris.ErrReplyReissueBaseAlreadyReissued) {
		t.Fatalf("nested reissue error = %v, want ErrReplyReissueBaseAlreadyReissued", err)
	}
}
