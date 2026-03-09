// Package server: Per-session 스트림 제한
package server

import (
	"log/slog"
	"sync"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
)

// StreamLimiter: 전역 및 per-session 스트림 제한
//
// 설계:
// - 전역 제한: 전체 동시 스트림 수 제한 (goroutine/FD 보호)
// - Per-session 제한: 세션당 동시 스트림 수 제한 (DoS 방지)
// - 3상태 모드: enforce(차단), monitor(로그만), off(건너뛰기)
type StreamLimiter struct {
	mu sync.Mutex

	// 전역 제한
	globalLimit   int64
	globalCurrent int64

	// Per-session 제한
	perSessionLimit int
	sessions        map[string]int

	// 모드 및 로깅
	mode   config.SecurityMode
	logger *slog.Logger
}

// NewStreamLimiter: StreamLimiter 생성
func NewStreamLimiter(globalLimit int64, perSessionLimit int, mode config.SecurityMode, logger *slog.Logger) *StreamLimiter {
	return &StreamLimiter{
		globalLimit:     globalLimit,
		perSessionLimit: perSessionLimit,
		sessions:        make(map[string]int),
		mode:            mode,
		logger:          logger,
	}
}

// TryAcquireResult: TryAcquire 결과
type TryAcquireResult struct {
	Acquired         bool
	GlobalLimitHit   bool
	PerSessionHitCnt int // 현재 세션의 스트림 수 (제한 초과 시)
}

// TryAcquire: 스트림 슬롯 획득 시도
//
// 반환값:
// - allowed=true: 스트림 진행 가능
// - allowed=false: 제한 초과로 거부 (enforce 모드만)
//
// 동작:
// - off 모드: 무조건 허용 (제한 없음)
// - monitor 모드: 제한 초과 시 로그만 남기고 허용
// - enforce 모드: 제한 초과 시 거부
func (l *StreamLimiter) TryAcquire(sessionID string) (allowed bool, result TryAcquireResult) {
	if l.mode == config.SecurityModeOff {
		return true, TryAcquireResult{Acquired: true}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	result = TryAcquireResult{}

	// 전역 제한 체크
	if l.globalCurrent >= l.globalLimit {
		result.GlobalLimitHit = true
	}

	// Per-session 제한 체크
	currentSessionStreams := l.sessions[sessionID]
	if currentSessionStreams >= l.perSessionLimit {
		result.PerSessionHitCnt = currentSessionStreams
	}

	// 제한 초과 여부
	limitExceeded := result.GlobalLimitHit || result.PerSessionHitCnt > 0

	if limitExceeded {
		if l.logger != nil {
			l.logger.Warn("stream_limit_exceeded",
				slog.String("session_id", sessionID),
				slog.Bool("global_limit_hit", result.GlobalLimitHit),
				slog.Int("session_current", currentSessionStreams),
				slog.Int("session_limit", l.perSessionLimit),
				slog.Int64("global_current", l.globalCurrent),
				slog.Int64("global_limit", l.globalLimit),
				slog.String("mode", string(l.mode)),
			)
		}

		if l.mode == config.SecurityModeEnforce {
			return false, result
		}
		// monitor 모드: 로그 남기고 허용 (아래에서 카운터 증가)
	}

	// 슬롯 획득
	l.globalCurrent++
	l.sessions[sessionID]++
	result.Acquired = true

	return true, result
}

// Release: 스트림 슬롯 해제
//
// 주의: 반드시 TryAcquire가 allowed=true를 반환한 경우에만 호출해야 함
// defer로 호출하는 것을 권장
func (l *StreamLimiter) Release(sessionID string) {
	if l.mode == config.SecurityModeOff {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.globalCurrent > 0 {
		l.globalCurrent--
	}

	if count, ok := l.sessions[sessionID]; ok {
		if count > 1 {
			l.sessions[sessionID]--
		} else {
			delete(l.sessions, sessionID)
		}
	}
}

// Stats: 현재 상태 반환 (디버깅/메트릭용)
func (l *StreamLimiter) Stats() (globalCurrent, globalLimit int64, sessionCount int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.globalCurrent, l.globalLimit, len(l.sessions)
}

// Mode: 현재 모드 반환
func (l *StreamLimiter) Mode() config.SecurityMode {
	return l.mode
}
