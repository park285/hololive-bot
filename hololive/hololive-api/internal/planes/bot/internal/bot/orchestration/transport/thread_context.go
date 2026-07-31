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
	"context"
	"strings"
	"sync/atomic"

	"github.com/park285/iris-client-go/iris"
)

type threadIDContextKey struct{}
type replyIdentityContextKey struct{}
type imageContentTypeContextKey struct{}

type replyIdentityState struct {
	id      string
	ordinal atomic.Uint64
}

func WithThreadID(ctx context.Context, threadID string) context.Context {
	id := strings.TrimSpace(threadID)
	if id == "" {
		return ctx
	}

	return context.WithValue(ctx, threadIDContextKey{}, id)
}

func ThreadIDFromContext(ctx context.Context) (string, bool) {
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

func appendThreadIDOption(ctx context.Context, opts []iris.SendOption) []iris.SendOption {
	if id, ok := ThreadIDFromContext(ctx); ok {
		return append(opts, iris.WithThreadID(id))
	}
	return opts
}

func WithImageContentType(ctx context.Context, contentType string) context.Context {
	value := strings.TrimSpace(contentType)
	if value == "" {
		return ctx
	}
	return context.WithValue(ctx, imageContentTypeContextKey{}, value)
}

func ImageContentTypeFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(imageContentTypeContextKey{}).(string)
	return value, ok && value != ""
}

func WithReplyIdentity(ctx context.Context, identity string) context.Context {
	id := strings.TrimSpace(identity)
	if id == "" {
		return ctx
	}

	return context.WithValue(ctx, replyIdentityContextKey{}, &replyIdentityState{id: id})
}

func ReplyIdentityFromContext(ctx context.Context) (string, bool) {
	state := replyIdentityStateFromContext(ctx)
	if state == nil {
		return "", false
	}

	return state.id, true
}

func replyIdentityStateFromContext(ctx context.Context) *replyIdentityState {
	if ctx == nil {
		return nil
	}

	state, ok := ctx.Value(replyIdentityContextKey{}).(*replyIdentityState)
	if !ok {
		return nil
	}

	return state
}

func nextReplyEmission(ctx context.Context) (identity string, ordinal uint64, ok bool) {
	state := replyIdentityStateFromContext(ctx)
	if state == nil {
		return "", 0, false
	}

	return state.id, state.ordinal.Add(1) - 1, true
}

func currentReplyEmission(ctx context.Context) (identity string, ordinal uint64, ok bool) {
	state := replyIdentityStateFromContext(ctx)
	if state == nil {
		return "", 0, false
	}
	ordinal = state.ordinal.Load()
	if ordinal == 0 {
		return state.id, 0, true
	}
	return state.id, ordinal - 1, true
}
