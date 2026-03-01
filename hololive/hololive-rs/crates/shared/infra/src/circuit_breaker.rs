use std::{
    sync::{
        Arc, Mutex,
        atomic::{AtomicU8, AtomicU32, Ordering},
    },
    time::{Duration, Instant},
};

use shared_core::error::SharedError;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CircuitState {
    Closed,
    Open,
    HalfOpen,
}

const STATE_CLOSED: u8 = 0;
const STATE_OPEN: u8 = 1;
const STATE_HALF_OPEN: u8 = 2;

struct Inner {
    failure_count: AtomicU32,
    threshold: u32,
    cached_state: AtomicU8,
    open_since: Mutex<Option<Instant>>,
    reset_duration: Duration,
}

impl std::fmt::Debug for Inner {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Inner")
            .field("failure_count", &self.failure_count.load(Ordering::Relaxed))
            .field("threshold", &self.threshold)
            .field("cached_state", &self.cached_state.load(Ordering::Relaxed))
            .field("open_since", &"<mutex>")
            .field("reset_duration", &self.reset_duration)
            .finish()
    }
}

#[derive(Clone, Debug)]
pub struct CircuitBreaker {
    inner: Arc<Inner>,
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

    fn check_open_expiry(&self) -> CircuitState {
        let Ok(mut guard) = self.inner.open_since.lock() else {
            return CircuitState::Open;
        };

        let should_transition = guard
            .as_ref()
            .is_none_or(|since| since.elapsed() >= self.inner.reset_duration);

        if should_transition {
            *guard = None;
            self.inner
                .cached_state
                .store(STATE_HALF_OPEN, Ordering::Release);
            return CircuitState::HalfOpen;
        }

        CircuitState::Open
    }

    pub fn state(&self) -> CircuitState {
        match self.inner.cached_state.load(Ordering::Acquire) {
            STATE_OPEN => self.check_open_expiry(),
            STATE_HALF_OPEN => CircuitState::HalfOpen,
            _ => CircuitState::Closed,
        }
    }

    pub fn allow_request(&self) -> Result<(), SharedError> {
        match self.state() {
            CircuitState::Open => Err(SharedError::CircuitOpen {
                platform: self.platform.clone(),
            }),
            _ => Ok(()),
        }
    }

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
            self.inner.failure_count.store(0, Ordering::Release);
        }
    }

    pub fn record_failure(&self) {
        let current = self.inner.cached_state.load(Ordering::Acquire);

        if current == STATE_HALF_OPEN {
            self.transition_to_open();
        } else if current == STATE_CLOSED {
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
