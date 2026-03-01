use std::{sync::atomic::Ordering, time::Duration};

use super::{SchedulerHealthSnapshot, SchedulerRuntimeHealth, duration_to_ms, now_ms};

impl SchedulerHealthSnapshot {
    pub fn overall_healthy(self) -> bool {
        self.youtube_healthy && self.chzzk_healthy && self.twitch_healthy
    }
}

impl SchedulerRuntimeHealth {
    pub(super) fn new(
        youtube_stale_after: Duration,
        chzzk_stale_after: Duration,
        twitch_stale_after: Duration,
        twitch_enabled: bool,
    ) -> Self {
        let now = now_ms();
        Self {
            youtube_last_beat_ms: std::sync::atomic::AtomicU64::new(now),
            chzzk_last_beat_ms: std::sync::atomic::AtomicU64::new(now),
            twitch_last_beat_ms: std::sync::atomic::AtomicU64::new(now),
            twitch_enabled,
            youtube_stale_after_ms: duration_to_ms(youtube_stale_after),
            chzzk_stale_after_ms: duration_to_ms(chzzk_stale_after),
            twitch_stale_after_ms: duration_to_ms(twitch_stale_after),
        }
    }

    pub(super) fn mark_youtube_beat(&self) {
        self.youtube_last_beat_ms.store(now_ms(), Ordering::Relaxed);
    }

    pub(super) fn mark_chzzk_beat(&self) {
        self.chzzk_last_beat_ms.store(now_ms(), Ordering::Relaxed);
    }

    pub(super) fn mark_twitch_beat(&self) {
        self.twitch_last_beat_ms.store(now_ms(), Ordering::Relaxed);
    }

    pub(super) fn mark_all_beats(&self) {
        let now = now_ms();
        self.youtube_last_beat_ms.store(now, Ordering::Relaxed);
        self.chzzk_last_beat_ms.store(now, Ordering::Relaxed);
        self.twitch_last_beat_ms.store(now, Ordering::Relaxed);
    }

    fn is_fresh(now: u64, last_beat_ms: u64, stale_after_ms: u64) -> bool {
        now.saturating_sub(last_beat_ms) <= stale_after_ms
    }

    pub fn snapshot(&self) -> SchedulerHealthSnapshot {
        let now = now_ms();
        let twitch_healthy = if self.twitch_enabled {
            Self::is_fresh(
                now,
                self.twitch_last_beat_ms.load(Ordering::Relaxed),
                self.twitch_stale_after_ms,
            )
        } else {
            true
        };
        SchedulerHealthSnapshot {
            youtube_healthy: Self::is_fresh(
                now,
                self.youtube_last_beat_ms.load(Ordering::Relaxed),
                self.youtube_stale_after_ms,
            ),
            chzzk_healthy: Self::is_fresh(
                now,
                self.chzzk_last_beat_ms.load(Ordering::Relaxed),
                self.chzzk_stale_after_ms,
            ),
            twitch_healthy,
        }
    }
}
