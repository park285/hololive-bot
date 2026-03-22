use crate::config::SecurityMode;
use std::collections::HashMap;
use std::sync::Mutex;

pub struct LimitResult {
    pub per_session_hit: bool,
    pub global_hit: bool,
}

pub struct StreamLimiter {
    global_limit: usize,
    per_session_limit: usize,
    mode: SecurityMode,
    state: Mutex<StreamState>,
}

struct StreamState {
    global_count: usize,
    per_session: HashMap<String, usize>,
}

impl StreamLimiter {
    pub fn new(global_limit: usize, per_session_limit: usize, mode: SecurityMode) -> Self {
        Self {
            global_limit,
            per_session_limit,
            mode,
            state: Mutex::new(StreamState {
                global_count: 0,
                per_session: HashMap::new(),
            }),
        }
    }

    pub fn try_acquire(&self, session_id: &str) -> (bool, LimitResult) {
        if self.mode == SecurityMode::Off {
            return (
                true,
                LimitResult {
                    per_session_hit: false,
                    global_hit: false,
                },
            );
        }

        let mut state = self.state.lock().unwrap();
        let session_count = state.per_session.get(session_id).copied().unwrap_or(0);

        let per_session_hit = session_count >= self.per_session_limit;
        let global_hit = state.global_count >= self.global_limit;

        let result = LimitResult {
            per_session_hit,
            global_hit,
        };

        if self.mode == SecurityMode::Monitor || (!per_session_hit && !global_hit) {
            state.global_count += 1;
            *state.per_session.entry(session_id.to_string()).or_insert(0) += 1;
            (true, result)
        } else {
            (false, result)
        }
    }

    pub fn release(&self, session_id: &str) {
        let mut state = self.state.lock().unwrap();
        state.global_count = state.global_count.saturating_sub(1);

        if let Some(count) = state.per_session.get_mut(session_id) {
            *count = count.saturating_sub(1);
            if *count == 0 {
                state.per_session.remove(session_id);
            }
        }
    }

    pub fn stats(&self) -> (usize, usize, usize) {
        let state = self.state.lock().unwrap();
        (state.global_count, self.global_limit, state.per_session.len())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::SecurityMode;

    #[test]
    fn test_acquire_and_release() {
        let limiter = StreamLimiter::new(10, 2, SecurityMode::Enforce);
        let (allowed, _) = limiter.try_acquire("session1");
        assert!(allowed);
        limiter.release("session1");
    }

    #[test]
    fn test_global_limit_enforced() {
        let limiter = StreamLimiter::new(2, 10, SecurityMode::Enforce);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s2").0);
        assert!(!limiter.try_acquire("s3").0);
    }

    #[test]
    fn test_per_session_limit_enforced() {
        let limiter = StreamLimiter::new(10, 2, SecurityMode::Enforce);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s1").0);
        let (allowed, result) = limiter.try_acquire("s1");
        assert!(!allowed);
        assert!(result.per_session_hit);
    }

    #[test]
    fn test_monitor_mode_allows_over_limit() {
        let limiter = StreamLimiter::new(1, 1, SecurityMode::Monitor);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s1").0);
    }

    #[test]
    fn test_off_mode_no_tracking() {
        let limiter = StreamLimiter::new(1, 1, SecurityMode::Off);
        assert!(limiter.try_acquire("s1").0);
        assert!(limiter.try_acquire("s1").0);
    }

    #[test]
    fn test_release_frees_slot() {
        let limiter = StreamLimiter::new(1, 1, SecurityMode::Enforce);
        assert!(limiter.try_acquire("s1").0);
        limiter.release("s1");
        assert!(limiter.try_acquire("s1").0);
    }

    #[test]
    fn test_stats() {
        let limiter = StreamLimiter::new(10, 2, SecurityMode::Enforce);
        limiter.try_acquire("s1");
        limiter.try_acquire("s2");
        let (global, limit, sessions) = limiter.stats();
        assert_eq!(global, 2);
        assert_eq!(limit, 10);
        assert_eq!(sessions, 2);
    }
}
