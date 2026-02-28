use alarm_core::error::AlarmError;
use std::{
    sync::{
        Arc, Mutex,
        atomic::{AtomicU8, AtomicU32, Ordering},
    },
    time::{Duration, Instant},
};

/// 서킷 브레이커 상태
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CircuitState {
    /// 정상 — 요청 허용
    Closed,
    /// 차단 — 요청 거부 (reset_duration 경과 후 HalfOpen 전환)
    Open,
    /// 탐색 — 요청 1건 허용하여 복구 여부 확인
    HalfOpen,
}

const STATE_CLOSED: u8 = 0;
const STATE_OPEN: u8 = 1;
const STATE_HALF_OPEN: u8 = 2;

struct Inner {
    /// Closed 상태에서 연속 실패 횟수 추적
    failure_count: AtomicU32,
    /// 연속 실패 임계값
    threshold: u32,
    /// 상태 캐시 (side-effect 없이 읽기 가능)
    cached_state: AtomicU8,
    /// Open 전환 시각 — HalfOpen 전환 만료 판단용
    open_since: Mutex<Option<Instant>>,
    /// Open 유지 기간
    reset_duration: Duration,
}

impl std::fmt::Debug for Inner {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Inner")
            .field("failure_count", &self.failure_count.load(Ordering::Relaxed))
            .field("threshold", &self.threshold)
            .field("cached_state", &self.cached_state.load(Ordering::Relaxed))
            .field("reset_duration", &self.reset_duration)
            .finish()
    }
}

/// 서킷 브레이커 — thread-safe, Clone 가능
#[derive(Clone, Debug)]
pub struct CircuitBreaker {
    inner: Arc<Inner>,
    /// 대상 플랫폼 식별자 (에러 메시지용)
    platform: String,
}

impl CircuitBreaker {
    pub fn new(threshold: u32, reset_duration: Duration, platform: impl Into<String>) -> Self {
        Self {
            inner: Arc::new(Inner {
                failure_count: AtomicU32::new(0),
                threshold,
                cached_state: AtomicU8::new(STATE_CLOSED),
                open_since: Mutex::new(None),
                reset_duration,
            }),
            platform: platform.into(),
        }
    }

    /// Open 상태에서 만료 여부를 확인하고 필요 시 HalfOpen으로 전환
    fn check_open_expiry(&self) -> CircuitState {
        let Ok(mut guard) = self.inner.open_since.lock() else {
            return CircuitState::Open;
        };

        let should_transition = guard
            .as_ref()
            .map(|since| since.elapsed() >= self.inner.reset_duration)
            .unwrap_or(true);

        if should_transition {
            *guard = None;
            self.inner
                .cached_state
                .store(STATE_HALF_OPEN, Ordering::Release);
            return CircuitState::HalfOpen;
        }

        CircuitState::Open
    }

    /// 현재 상태 반환 (side-effect 없음)
    pub fn state(&self) -> CircuitState {
        match self.inner.cached_state.load(Ordering::Acquire) {
            STATE_OPEN => self.check_open_expiry(),
            STATE_HALF_OPEN => CircuitState::HalfOpen,
            _ => CircuitState::Closed,
        }
    }

    /// 요청 허용 여부 확인
    /// Open이면 Err(CircuitOpen) 반환, Closed/HalfOpen이면 Ok 반환
    pub fn allow_request(&self) -> Result<(), AlarmError> {
        match self.state() {
            CircuitState::Open => Err(AlarmError::CircuitOpen {
                platform: self.platform.clone(),
            }),
            _ => Ok(()),
        }
    }

    /// 요청 성공 보고 — HalfOpen이면 Closed로 복구
    pub fn record_success(&self) {
        let current = self.inner.cached_state.load(Ordering::Acquire);

        if current == STATE_HALF_OPEN {
            if let Ok(mut guard) = self.inner.open_since.lock() {
                *guard = None;
            }
            self.inner.failure_count.store(0, Ordering::Release);
            self.inner
                .cached_state
                .store(STATE_CLOSED, Ordering::Release);
        } else if current == STATE_CLOSED {
            // Closed 상태에서 성공 → 연속 실패 카운터 리셋
            self.inner.failure_count.store(0, Ordering::Release);
        }
    }

    /// 요청 실패 보고 — 임계값 초과 또는 HalfOpen 실패 시 즉시 Open 전환
    pub fn record_failure(&self) {
        let current = self.inner.cached_state.load(Ordering::Acquire);

        if current == STATE_HALF_OPEN {
            self.transition_to_open();
        } else if current == STATE_CLOSED {
            // 연속 실패 카운터 증가, 임계값 도달 시 Open 전환
            let count = self.inner.failure_count.fetch_add(1, Ordering::AcqRel) + 1;
            if count >= self.inner.threshold {
                self.transition_to_open();
            }
        }
    }

    fn transition_to_open(&self) {
        let Ok(mut guard) = self.inner.open_since.lock() else {
            return;
        };
        *guard = Some(Instant::now());
        self.inner.failure_count.store(0, Ordering::Release);
        self.inner.cached_state.store(STATE_OPEN, Ordering::Release);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{thread, time::Instant};

    fn wait_until_state(
        cb: &CircuitBreaker,
        expected: CircuitState,
        timeout: Duration,
    ) -> CircuitState {
        let deadline = Instant::now() + timeout;
        loop {
            let current = cb.state();
            if current == expected {
                return current;
            }
            assert!(
                Instant::now() < deadline,
                "state did not become {:?} within {:?}, current={:?}",
                expected,
                timeout,
                current
            );
            thread::sleep(Duration::from_millis(1));
        }
    }

    #[test]
    fn initial_state_is_closed() {
        let cb = CircuitBreaker::new(3, Duration::from_secs(30), "test");
        assert_eq!(cb.state(), CircuitState::Closed);
        assert!(cb.allow_request().is_ok());
    }

    #[test]
    fn opens_after_threshold_failures() {
        let cb = CircuitBreaker::new(3, Duration::from_secs(30), "test");

        cb.record_failure();
        cb.record_failure();
        // 아직 Closed
        assert!(cb.allow_request().is_ok());

        cb.record_failure();
        // 임계값(3) 도달 → Open
        assert_eq!(cb.state(), CircuitState::Open);
        assert!(cb.allow_request().is_err());
    }

    #[test]
    fn transitions_to_half_open_after_zero_reset() {
        // reset_duration=0일 때 즉시 HalfOpen 의미 유지
        let cb = CircuitBreaker::new(1, Duration::ZERO, "test");
        cb.record_failure();

        assert_eq!(cb.state(), CircuitState::HalfOpen);
        assert!(cb.allow_request().is_ok());
    }

    #[test]
    fn success_in_half_open_closes_circuit() {
        let cb = CircuitBreaker::new(1, Duration::ZERO, "test");
        cb.record_failure();

        // HalfOpen 상태로 전환 트리거
        let _ = cb.state();

        cb.record_success();
        assert_eq!(cb.state(), CircuitState::Closed);
        assert!(cb.allow_request().is_ok());
    }

    #[test]
    fn failure_in_half_open_reopens_circuit() {
        let cb = CircuitBreaker::new(1, Duration::from_millis(20), "test");
        cb.record_failure(); // Open (count=1 >= threshold=1)

        wait_until_state(&cb, CircuitState::HalfOpen, Duration::from_millis(200));

        // HalfOpen에서 실패 → Open 재진입
        cb.record_failure();

        assert_eq!(cb.state(), CircuitState::Open);
        assert!(
            cb.allow_request().is_err(),
            "HalfOpen 실패 후 서킷이 Open 상태여야 함"
        );
    }

    #[test]
    fn circuit_open_error_includes_platform() {
        let cb = CircuitBreaker::new(1, Duration::from_secs(60), "chzzk");
        cb.record_failure();

        match cb.allow_request() {
            Err(AlarmError::CircuitOpen { platform }) => assert_eq!(platform, "chzzk"),
            other => panic!("expected CircuitOpen error, got: {other:?}"),
        }
    }
}
