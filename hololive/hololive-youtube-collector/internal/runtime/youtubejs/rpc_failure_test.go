package youtubejs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestHelperRetryPreservesCauseAndHint(t *testing.T) {
	t.Parallel()

	cause := errors.New("provider limited")
	base := collecterr.Wrap(collecterr.Cooldown, collecterr.ClassCooldown, cause)
	at := time.Date(2026, time.October, 2, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		retry RPCRetryHint
		kind  collecterr.RetryHintKind
	}{
		{retry: RPCRetryHint{Kind: "default"}, kind: collecterr.RetryDefault},
		{retry: RPCRetryHint{Kind: "after", AfterMS: 15000}, kind: collecterr.RetryAfter},
		{retry: RPCRetryHint{Kind: "at", At: at.Format(time.RFC3339)}, kind: collecterr.RetryAt},
	}

	for _, test := range tests {
		t.Run(string(test.retry.Kind), func(t *testing.T) {
			t.Parallel()

			err := applyHelperRetry(base, test.retry)
			if !errors.Is(err, cause) || collecterr.CodeOf(err) != collecterr.Cooldown || collecterr.IsUnclassified(err) {
				t.Fatalf("lost classified cause: %v", err)
			}

			hint := collecterr.RetryOf(err)
			if hint.Kind() != test.kind {
				t.Fatalf("retry kind = %s, want %s", hint.Kind(), test.kind)
			}

			if test.kind == collecterr.RetryAfter && hint.After() != 15*time.Second {
				t.Fatalf("retry after = %s", hint.After())
			}

			if test.kind == collecterr.RetryAt && !hint.At().Equal(at) {
				t.Fatalf("retry at = %s", hint.At())
			}
		})
	}
}

func TestHelperStatusErrorKeepsProtocolCauseAndMasksMessage(t *testing.T) {
	t.Parallel()

	invalid := helperStatusError(http.StatusTooManyRequests, []byte(
		`{"protocol_version":1,"error":{"code":"cooldown","class":"COOLDOWN","retry":{"kind":"at","at":"invalid"},"message":"limited"}}`,
	))
	assertProtocolMismatch(t, invalid)

	if _, ok := errors.AsType[*time.ParseError](invalid); !ok {
		t.Fatalf("lost retry timestamp parse error: %v", invalid)
	}

	const secret = "private-query-value"

	err := helperStatusError(http.StatusTooManyRequests, []byte(fmt.Sprintf(
		`{"protocol_version":1,"error":{"code":"cooldown","class":"COOLDOWN","retry":{"kind":"default"},"message":"https://example.test/path?token=%s"}}`, secret,
	)))
	if collecterr.CodeOf(err) != collecterr.Cooldown || collecterr.ClassOf(err) != collecterr.ClassCooldown || collecterr.IsUnclassified(err) {
		t.Fatalf("lost explicit helper failure: %v", err)
	}

	if strings.Contains(err.Error(), secret) {
		t.Fatalf("unmasked helper message: %v", err)
	}
}

func TestHelperBodyReadFailurePreservesCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	body := &failedHelperBody{Reader: iotest.ErrReader(context.DeadlineExceeded), closeErr: closeErr}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       body,
	}

	err := decodeHelperResponse(resp, 1024, &CommunityResult{})
	if collecterr.CodeOf(err) != collecterr.Timeout || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, closeErr) {
		t.Fatalf("lost body read or close failure: %v", err)
	}

	if body.closes != 1 {
		t.Fatalf("closes = %d", body.closes)
	}
}

type failedHelperBody struct {
	io.Reader

	closeErr error
	closes   int
}

func (b *failedHelperBody) Close() error {
	b.closes++

	return b.closeErr
}
