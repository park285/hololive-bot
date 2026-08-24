// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package transport

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplyClientRequestIDShape(t *testing.T) {
	t.Parallel()

	got := replyClientRequestID("message:m-1", 0)
	assert.Equal(t, "hololive:v1:message:m-1:reply:0", got)
	assert.True(t, isValidReplyClientRequestID(got))
}

func TestReplyClientRequestIDIsStable(t *testing.T) {
	t.Parallel()

	t.Run("same message and ordinal yield the same id", func(t *testing.T) {
		t.Parallel()

		first := replyClientRequestID("message:m-1", 0)
		second := replyClientRequestID("message:m-1", 0)
		assert.Equal(t, first, second)
	})

	t.Run("ordinal separates emissions within one inbound message", func(t *testing.T) {
		t.Parallel()

		assert.NotEqual(t,
			replyClientRequestID("message:m-1", 0),
			replyClientRequestID("message:m-1", 1),
		)
	})

	t.Run("different inbound messages yield different ids", func(t *testing.T) {
		t.Parallel()

		assert.NotEqual(t,
			replyClientRequestID("message:m-1", 0),
			replyClientRequestID("message:m-2", 0),
		)
	})

	t.Run("blank message id yields no id", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, replyClientRequestID("", 0))
		assert.Empty(t, replyClientRequestID("   ", 0))
	})

	t.Run("surrounding whitespace on the message id is irrelevant", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t,
			replyClientRequestID("message:m-1", 0),
			replyClientRequestID("  message:m-1  ", 0),
		)
	})
}

func TestReissuedReplyClientRequestID(t *testing.T) {
	t.Parallel()

	const base = "hololive:v1:message:m-1:reply:0"

	assert.Equal(t, base, reissuedReplyClientRequestID(base, 0))
	assert.Equal(t, base+":r1", reissuedReplyClientRequestID(base, 1))
	assert.Equal(t, base+":r2", reissuedReplyClientRequestID(base, 2))
	assert.Empty(t, reissuedReplyClientRequestID("", 1))

	maxBase := strings.Repeat("a", replyClientRequestIDMaxLen)
	oversized := reissuedReplyClientRequestID(maxBase, 1)
	assert.True(t, isValidReplyClientRequestID(oversized))
	assert.True(t, strings.HasSuffix(oversized, ":r1"))
	assert.Equal(t, oversized, reissuedReplyClientRequestID(maxBase, 1))
}

func TestReplyClientRequestIDIgnoresBody(t *testing.T) {
	t.Parallel()

	sendAndCaptureID := func(t *testing.T, room, message string) string {
		t.Helper()

		c := &stubBotClient{}
		ctx := WithReplyIdentity(t.Context(), "message:m-1")
		require.NoError(t, NewCommandTransport(c, nil).SendMessage(ctx, room, message))

		id, _ := capturedSendOptions(t, c.lastOpts)

		return id
	}

	baseline := sendAndCaptureID(t, "room-1", "hello")
	require.NotEmpty(t, baseline)

	for _, message := range []string{"완전히 다른 본문", strings.Repeat("x", 4096), " hello ", ""} {
		assert.Equal(t, baseline, sendAndCaptureID(t, "room-1", message),
			"reply body must never take part in the id")
	}

	assert.Equal(t, baseline, sendAndCaptureID(t, "room-2", "hello"),
		"room must never take part in the id")
}

func TestReplyClientRequestIDHonoursIrisConstraints(t *testing.T) {
	t.Parallel()

	t.Run("oversized message id falls back to a hashed token", func(t *testing.T) {
		t.Parallel()

		long := strings.Repeat("m", 400)
		got := replyClientRequestID(long, 0)
		require.True(t, isValidReplyClientRequestID(got), "id %q violates the iris contract", got)
		assert.Contains(t, got, hashedReplyIDToken(long))
		assert.Equal(t, got, replyClientRequestID(long, 0))
	})

	t.Run("illegal characters fall back to a hashed token", func(t *testing.T) {
		t.Parallel()

		raw := "message:닉네임 with/slash?and=query"
		got := replyClientRequestID(raw, 0)
		require.True(t, isValidReplyClientRequestID(got), "id %q violates the iris contract", got)
		assert.Contains(t, got, hashedReplyIDToken(raw))
		assert.Equal(t, got, replyClientRequestID(raw, 0))
	})

	t.Run("large ordinals stay within the length budget", func(t *testing.T) {
		t.Parallel()

		got := replyClientRequestID("message:m-1", ^uint64(0))
		assert.True(t, isValidReplyClientRequestID(got), "id %q violates the iris contract", got)
	})
}

func TestIsValidReplyClientRequestID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"canonical id", "hololive:v1:message:m-1:reply:0", true},
		{"dot underscore dash are allowed", "hololive:v1:a_b.c-d:reply:0", true},
		{"too short", "short", false},
		{"too long", strings.Repeat("a", 161), false},
		{"space is rejected", "hololive:v1:a b:reply:0", false},
		{"slash is rejected", "hololive:v1:a/b:reply:0", false},
		{"non ascii is rejected", "hololive:v1:한글:reply:0", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, isValidReplyClientRequestID(tc.id))
		})
	}
}

func TestNextReplyClientRequestID(t *testing.T) {
	t.Parallel()

	t.Run("no inbound identity yields no id", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, nextReplyClientRequestID(t.Context()))
	})

	t.Run("ordinal advances per emission within one context", func(t *testing.T) {
		t.Parallel()

		ctx := WithReplyIdentity(t.Context(), "message:m-1")
		assert.Equal(t, "hololive:v1:message:m-1:reply:0", nextReplyClientRequestID(ctx))
		assert.Equal(t, "hololive:v1:message:m-1:reply:1", nextReplyClientRequestID(ctx))
		assert.Equal(t, "hololive:v1:message:m-1:reply:2", nextReplyClientRequestID(ctx))
	})

	t.Run("a redelivered inbound message restarts at ordinal zero", func(t *testing.T) {
		t.Parallel()

		first := nextReplyClientRequestID(WithReplyIdentity(t.Context(), "message:m-1"))
		second := nextReplyClientRequestID(WithReplyIdentity(t.Context(), "message:m-1"))
		assert.Equal(t, first, second)
	})

	t.Run("derived contexts share one ordinal sequence", func(t *testing.T) {
		t.Parallel()

		ctx := WithReplyIdentity(t.Context(), "message:m-1")
		derived := WithThreadID(ctx, "t-1")

		assert.Equal(t, "hololive:v1:message:m-1:reply:0", nextReplyClientRequestID(ctx))
		assert.Equal(t, "hololive:v1:message:m-1:reply:1", nextReplyClientRequestID(derived))
	})

	t.Run("concurrent emissions never collide", func(t *testing.T) {
		t.Parallel()

		const emissions = 64

		ctx := WithReplyIdentity(t.Context(), "message:m-1")

		var (
			mu sync.Mutex
			wg sync.WaitGroup
		)

		seen := make(map[string]struct{}, emissions)

		for range emissions {
			wg.Go(func() {
				id := nextReplyClientRequestID(ctx)

				mu.Lock()
				defer mu.Unlock()

				seen[id] = struct{}{}
			})
		}

		wg.Wait()

		assert.Len(t, seen, emissions)
	})
}
