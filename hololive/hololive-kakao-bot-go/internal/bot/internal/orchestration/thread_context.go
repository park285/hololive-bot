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

package orchestration

import (
	"context"
	"strings"
)

type threadIDContextKey struct{}
type replyIdentityContextKey struct{}

func withThreadID(ctx context.Context, threadID string) context.Context {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return ctx
	}

	return context.WithValue(ctx, threadIDContextKey{}, id)
}

func threadIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	raw := ctx.Value(threadIDContextKey{})

	id, ok := raw.(string)
	if !ok {
		return "", false
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}

	return id, true
}

func withReplyIdentity(ctx context.Context, identity string) context.Context {
	id := strings.TrimSpace(identity)
	if id == "" {
		return ctx
	}

	return context.WithValue(ctx, replyIdentityContextKey{}, id)
}

func replyIdentityFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}

	raw := ctx.Value(replyIdentityContextKey{})

	id, ok := raw.(string)
	if !ok {
		return "", false
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}

	return id, true
}
