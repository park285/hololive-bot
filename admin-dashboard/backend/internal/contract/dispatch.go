package contract

import (
	"context"
	"sync/atomic"
)

type dispatchKey struct{}

// Dispatch는 최초 mutation 선점과 upstream 전송 시도를 기록하며 수신·효과·취소를 증명하지 않습니다.
type Dispatch struct {
	attempted  atomic.Bool
	mutationID atomic.Pointer[string]
}

// WithDispatch는 HTTP 경계의 요청 기록을 adapter의 전송 지점에 연결합니다.
func WithDispatch(ctx context.Context, state *Dispatch) context.Context {
	return context.WithValue(ctx, dispatchKey{}, state)
}

// MarkDispatched는 I/O 직전에 호출하여 전송 이후의 불확실성을 보존합니다.
func MarkDispatched(ctx context.Context) {
	if state, ok := ctx.Value(dispatchKey{}).(*Dispatch); ok {
		state.attempted.Store(true)
	}
}

// MarkMutationClaimed는 인증·CSRF 후 family의 ID를 최초로 원자 선점한 경우에만 호출합니다.
func MarkMutationClaimed(ctx context.Context, id string) {
	if state, ok := ctx.Value(dispatchKey{}).(*Dispatch); ok {
		state.mutationID.Store(&id)
	}
}

// NotDispatchedMutationID는 C04의 최초 선점 후 전송 전 거부 근거이며 receipt가 아닙니다.
func NotDispatchedMutationID(ctx context.Context) string {
	state, ok := ctx.Value(dispatchKey{}).(*Dispatch)
	if !ok || state.Attempted() {
		return ""
	}

	if id := state.mutationID.Load(); id != nil {
		return *id
	}

	return ""
}

// Attempted는 최소 한 번의 전송 시도 여부를 반환합니다.
func (d *Dispatch) Attempted() bool { return d.attempted.Load() }
