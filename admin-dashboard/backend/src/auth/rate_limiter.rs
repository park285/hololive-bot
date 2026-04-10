use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tokio_util::sync::CancellationToken;

struct AttemptInfo {
    count: usize,
    first_attempt: Instant,
    locked_until: Option<Instant>,
}

#[allow(missing_debug_implementations)]
pub struct LoginRateLimiter {
    attempts: Mutex<HashMap<String, AttemptInfo>>,
    max_attempts: usize,
    window: Duration,
    lockout: Duration,
    cancel: CancellationToken,
}

impl Default for LoginRateLimiter {
    fn default() -> Self {
        Self::new()
    }
}

impl LoginRateLimiter {
    pub fn new() -> Self {
        Self {
            attempts: Mutex::new(HashMap::new()),
            max_attempts: 5,
            window: Duration::from_secs(5 * 60),
            lockout: Duration::from_secs(15 * 60),
            cancel: CancellationToken::new(),
        }
    }

    pub fn is_allowed(&self, ip: &str) -> (bool, Duration) {
        let mut attempts = self.attempts.lock().unwrap();
        let Some(info) = attempts.get_mut(ip) else {
            return (true, Duration::ZERO);
        };

        if let Some(locked_until) = info.locked_until {
            let now = Instant::now();
            if now < locked_until {
                return (false, locked_until - now);
            }
            attempts.remove(ip);
            return (true, Duration::ZERO);
        }

        if info.first_attempt.elapsed() > self.window {
            attempts.remove(ip);
            return (true, Duration::ZERO);
        }

        if info.count < self.max_attempts {
            return (true, Duration::ZERO);
        }

        (false, Duration::ZERO)
    }

    pub fn record_failure(&self, ip: &str) -> usize {
        let mut attempts = self.attempts.lock().unwrap();
        let info = attempts
            .entry(ip.to_string())
            .or_insert_with(|| AttemptInfo {
                count: 0,
                first_attempt: Instant::now(),
                locked_until: None,
            });

        info.count += 1;

        if info.count >= self.max_attempts {
            info.locked_until = Some(Instant::now() + self.lockout);
        }

        info.count
    }

    pub fn record_success(&self, ip: &str) {
        let mut attempts = self.attempts.lock().unwrap();
        attempts.remove(ip);
    }

    pub fn start_cleanup_task(self: &Arc<Self>) {
        let limiter = Arc::clone(self);
        let cancel = self.cancel.clone();
        tokio::spawn(async move {
            loop {
                tokio::select! {
                    () = cancel.cancelled() => break,
                    () = tokio::time::sleep(Duration::from_secs(60)) => {
                        let mut attempts = limiter.attempts.lock().unwrap();
                        let now = Instant::now();
                        attempts.retain(|_, info| {
                            if let Some(locked_until) = info.locked_until
                                && now >= locked_until
                            {
                                return false;
                            }
                            if info.first_attempt.elapsed() > limiter.window {
                                return false;
                            }
                            true
                        });
                    }
                }
            }
        });
    }

    pub fn shutdown(&self) {
        self.cancel.cancel();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_first_attempt_allowed() {
        let rl = LoginRateLimiter::new();
        let (allowed, _) = rl.is_allowed("1.2.3.4");
        assert!(allowed);
    }

    #[tokio::test]
    async fn test_lockout_after_max_failures() {
        let rl = LoginRateLimiter::new();
        for _ in 0..5 {
            rl.record_failure("1.2.3.4");
        }
        let (allowed, remaining) = rl.is_allowed("1.2.3.4");
        assert!(!allowed);
        assert!(remaining > Duration::ZERO);
    }

    #[tokio::test]
    async fn test_success_resets_count() {
        let rl = LoginRateLimiter::new();
        rl.record_failure("1.2.3.4");
        rl.record_failure("1.2.3.4");
        rl.record_success("1.2.3.4");
        let (allowed, _) = rl.is_allowed("1.2.3.4");
        assert!(allowed);
    }

    #[tokio::test]
    async fn test_different_ips_independent() {
        let rl = LoginRateLimiter::new();
        for _ in 0..5 {
            rl.record_failure("1.2.3.4");
        }
        let (allowed, _) = rl.is_allowed("5.6.7.8");
        assert!(allowed);
    }

    #[tokio::test]
    async fn test_record_failure_returns_count() {
        let rl = LoginRateLimiter::new();
        assert_eq!(rl.record_failure("1.2.3.4"), 1);
        assert_eq!(rl.record_failure("1.2.3.4"), 2);
    }
}
