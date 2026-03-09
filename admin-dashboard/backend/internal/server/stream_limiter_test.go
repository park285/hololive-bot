package server

import (
	"sync"
	"testing"

	"github.com/park285/llm-kakao-bots/admin-dashboard/internal/config"
)

func TestStreamLimiter_AcquireRelease(t *testing.T) {
	limiter := NewStreamLimiter(10, 2, config.SecurityModeEnforce, nil)

	// 획득
	allowed, result := limiter.TryAcquire("session1")
	if !allowed {
		t.Fatal("TryAcquire() returned false, want true")
	}
	if !result.Acquired {
		t.Error("result.Acquired = false, want true")
	}

	// 상태 확인
	global, globalMax, sessions := limiter.Stats()
	if global != 1 {
		t.Errorf("globalCurrent = %d, want 1", global)
	}
	if globalMax != 10 {
		t.Errorf("globalLimit = %d, want 10", globalMax)
	}
	if sessions != 1 {
		t.Errorf("sessionCount = %d, want 1", sessions)
	}

	// 해제
	limiter.Release("session1")
	global, _, sessions = limiter.Stats()
	if global != 0 {
		t.Errorf("after Release: globalCurrent = %d, want 0", global)
	}
	if sessions != 0 {
		t.Errorf("after Release: sessionCount = %d, want 0", sessions)
	}
}

func TestStreamLimiter_PerSessionLimit(t *testing.T) {
	limiter := NewStreamLimiter(10, 2, config.SecurityModeEnforce, nil)

	// 세션당 2개까지 허용
	allowed1, _ := limiter.TryAcquire("session1")
	allowed2, _ := limiter.TryAcquire("session1")
	allowed3, result := limiter.TryAcquire("session1")

	if !allowed1 || !allowed2 {
		t.Fatal("first 2 acquires should succeed")
	}

	if allowed3 {
		t.Error("3rd acquire should fail due to per-session limit")
	}

	if result.PerSessionHitCnt != 2 {
		t.Errorf("result.PerSessionHitCnt = %d, want 2", result.PerSessionHitCnt)
	}

	// 다른 세션은 OK
	allowedOther, _ := limiter.TryAcquire("session2")
	if !allowedOther {
		t.Error("different session should succeed")
	}

	// 해제 후 다시 획득 가능
	limiter.Release("session1")
	allowed4, _ := limiter.TryAcquire("session1")
	if !allowed4 {
		t.Error("after Release, acquire should succeed again")
	}
}

func TestStreamLimiter_GlobalLimit(t *testing.T) {
	limiter := NewStreamLimiter(3, 10, config.SecurityModeEnforce, nil)

	// 전역 3개까지 허용
	limiter.TryAcquire("s1")
	limiter.TryAcquire("s2")
	limiter.TryAcquire("s3")

	allowed, result := limiter.TryAcquire("s4")
	if allowed {
		t.Error("4th acquire should fail due to global limit")
	}

	if !result.GlobalLimitHit {
		t.Error("result.GlobalLimitHit = false, want true")
	}
}

func TestStreamLimiter_MonitorMode(t *testing.T) {
	limiter := NewStreamLimiter(1, 1, config.SecurityModeMonitor, nil)

	// 첫 번째: 정상 획득
	allowed1, _ := limiter.TryAcquire("session1")
	if !allowed1 {
		t.Fatal("first acquire should succeed")
	}

	// 두 번째: monitor 모드에서는 제한 초과해도 허용
	allowed2, result := limiter.TryAcquire("session1")
	if !allowed2 {
		t.Error("monitor mode should allow even when limit exceeded")
	}

	// per-session 제한 히트됐지만 허용됨
	if result.PerSessionHitCnt != 1 {
		t.Errorf("result.PerSessionHitCnt = %d, want 1", result.PerSessionHitCnt)
	}
}

func TestStreamLimiter_OffMode(t *testing.T) {
	limiter := NewStreamLimiter(0, 0, config.SecurityModeOff, nil)

	// off 모드에서는 제한 없이 허용
	for i := range 100 {
		allowed, _ := limiter.TryAcquire("session")
		if !allowed {
			t.Fatalf("off mode should always allow (failed at %d)", i)
		}
	}

	// Release도 무시됨
	limiter.Release("session")

	global, _, _ := limiter.Stats()
	if global != 0 {
		t.Errorf("off mode: globalCurrent = %d, want 0 (no tracking)", global)
	}
}

func TestStreamLimiter_ConcurrentAccess(t *testing.T) {
	// go test -race로 실행 필수
	limiter := NewStreamLimiter(100, 50, config.SecurityModeEnforce, nil)

	var wg sync.WaitGroup
	testSessions := []string{"s1", "s2", "s3", "s4", "s5"}
	iterations := 100

	for _, sessionID := range testSessions {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			for range iterations {
				allowed, _ := limiter.TryAcquire(sid)
				if allowed {
					// 약간의 작업 시뮬레이션
					for j := range 10 {
						_ = j * j
					}
					limiter.Release(sid)
				}
			}
		}(sessionID)
	}

	wg.Wait()

	// 모든 슬롯 해제 확인
	globalCnt, _, sessionCnt := limiter.Stats()
	if globalCnt != 0 {
		t.Errorf("after all goroutines: globalCurrent = %d, want 0", globalCnt)
	}
	if sessionCnt != 0 {
		t.Errorf("after all goroutines: sessionCount = %d, want 0", sessionCnt)
	}
}

func TestStreamLimiter_ReleaseWithoutAcquire(t *testing.T) {
	// Release를 획득 없이 호출해도 패닉하지 않음
	limiter := NewStreamLimiter(10, 2, config.SecurityModeEnforce, nil)

	// 패닉 없이 완료되어야 함
	limiter.Release("nonexistent")

	global, _, _ := limiter.Stats()
	if global != 0 {
		t.Errorf("globalCurrent = %d, want 0", global)
	}
}

func TestStreamLimiter_MultipleSessionsIsolated(t *testing.T) {
	limiter := NewStreamLimiter(10, 2, config.SecurityModeEnforce, nil)

	// 세션 1: 2개 획득
	limiter.TryAcquire("s1")
	limiter.TryAcquire("s1")

	// 세션 2: 2개 획득 (독립)
	allowed1, _ := limiter.TryAcquire("s2")
	allowed2, _ := limiter.TryAcquire("s2")

	if !allowed1 || !allowed2 {
		t.Error("session2 should be isolated from session1 limits")
	}

	// 세션 1 해제가 세션 2에 영향 없음
	limiter.Release("s1")
	allowed3, _ := limiter.TryAcquire("s2") // s2는 여전히 2개 사용 중
	if allowed3 {
		t.Error("session2 should still be at limit")
	}

	// Stats 검증 (별도 변수명 사용)
	// s1: 2-1=1개, s2: 2개 → 총 3개
	globalCnt, globalMax, sessionCnt := limiter.Stats()
	if globalCnt != 3 {
		t.Errorf("globalCurrent = %d, want 3", globalCnt)
	}
	if globalMax != 10 {
		t.Errorf("globalLimit = %d, want 10", globalMax)
	}
	if sessionCnt != 2 {
		t.Errorf("sessionCount = %d, want 2", sessionCnt)
	}
}
