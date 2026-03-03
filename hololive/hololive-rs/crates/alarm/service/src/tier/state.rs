use chrono::{DateTime, Utc};

// ─────────────────────────────────────────────────────────────────────────────
// 채널 스케줄 상태 (채널별 폴링 시각 관리)
// ─────────────────────────────────────────────────────────────────────────────

/// 채널 단위 Tier 스케줄 상태
pub(super) struct ChannelScheduleState {
    /// 다음 체크 예정 시각
    next_check_at: DateTime<Utc>,
    /// 마지막으로 체크된 시각
    last_checked_at: DateTime<Utc>,
    /// 가장 가까운 예정 시작 시각
    nearest_start_at: Option<DateTime<Utc>>,
    /// 즉시 체크 강제 플래그
    force_due: bool,
    /// 마지막으로 가져온 스트림 수
    last_streams_count: usize,
    /// 마지막 알림 발송 시각 (고빈도 폴링 유지 판단용)
    last_notified_at: Option<DateTime<Utc>>,
}

impl ChannelScheduleState {
    pub(super) fn new(
        next_check_at: DateTime<Utc>,
        last_checked_at: DateTime<Utc>,
        nearest_start_at: Option<DateTime<Utc>>,
        force_due: bool,
        last_streams_count: usize,
        last_notified_at: Option<DateTime<Utc>>,
    ) -> Self {
        Self {
            next_check_at,
            last_checked_at,
            nearest_start_at,
            force_due,
            last_streams_count,
            last_notified_at,
        }
    }

    pub(super) fn is_due(&self, now: DateTime<Utc>) -> bool {
        self.force_due || now >= self.next_check_at
    }

    pub(super) fn set_next_check_at(&mut self, next_check_at: DateTime<Utc>) {
        self.next_check_at = next_check_at;
    }

    pub(super) fn set_last_checked_at(&mut self, last_checked_at: DateTime<Utc>) {
        self.last_checked_at = last_checked_at;
    }

    pub(super) fn set_nearest_start_at(&mut self, nearest_start_at: Option<DateTime<Utc>>) {
        self.nearest_start_at = nearest_start_at;
    }

    pub(super) fn set_force_due(&mut self, force_due: bool) {
        self.force_due = force_due;
    }

    pub(super) fn set_last_streams_count(&mut self, last_streams_count: usize) {
        self.last_streams_count = last_streams_count;
    }

    pub(super) fn set_last_notified_at(&mut self, last_notified_at: Option<DateTime<Utc>>) {
        self.last_notified_at = last_notified_at;
    }

    #[cfg(test)]
    pub(super) fn nearest_start_at(&self) -> Option<DateTime<Utc>> {
        self.nearest_start_at
    }

    pub(super) fn last_notified_at(&self) -> Option<DateTime<Utc>> {
        self.last_notified_at
    }
}
