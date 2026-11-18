package transport

import (
	"context"
	"strings"
	"sync/atomic"
)

type (
	threadIDContextKey         struct{}
	replyIdentityContextKey    struct{}
	imageContentTypeContextKey struct{}
)

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

func WithImageContentType(ctx context.Context, contentType string) context.Context {
	value := strings.TrimSpace(contentType)
	if value == "" {
		return ctx
	}

	return context.WithValue(ctx, imageContentTypeContextKey{}, value)
}

func ImageContentTypeFromContext(ctx context.Context) (string, bool) {
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

type replyEmission struct {
	identity        string
	ordinal         uint64
	clientRequestID string
	ok              bool
}

func issueReplyEmission(ctx context.Context) replyEmission {
	identity, ordinal, ok := nextReplyEmission(ctx)
	if !ok {
		return replyEmission{}
	}

	return replyEmission{
		identity:        identity,
		ordinal:         ordinal,
		clientRequestID: replyClientRequestID(identity, ordinal),
		ok:              true,
	}
}
