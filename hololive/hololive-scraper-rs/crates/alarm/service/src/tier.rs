use alarm_core::{
    constants::{
        FULL_REFRESH_INTERVAL, NO_UPCOMING_INTERVAL, RECENTLY_NOTIFIED_WINDOW, TIER1_INTERVAL,
        TIER1_WINDOW, TIER2_INTERVAL, TIER2_WINDOW, TIER3_INTERVAL, TIER3_WINDOW, TIER4_INTERVAL,
    },
    model::Stream,
};
use chrono::{DateTime, Utc};
use dashmap::DashMap;
use parking_lot::Mutex;

// ─────────────────────────────────────────────────────────────────────────────
// 채널 스케줄 상태 (채널별 폴링 시각 관리)
// ─────────────────────────────────────────────────────────────────────────────

/// 채널 단위 Tier 스케줄 상태
struct ChannelScheduleState {
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

// ─────────────────────────────────────────────────────────────────────────────
// TieredScheduler: 채널별 폴링 빈도를 방송 임박도에 따라 동적으로 조절
// ─────────────────────────────────────────────────────────────────────────────

/// Tier 기반 채널 스케줄러
/// DashMap으로 채널 상태를 동시성 안전하게 관리한다.
pub struct TieredScheduler {
    /// 채널별 스케줄 상태 (channel_id → state)
    states: DashMap<String, ChannelScheduleState>,
    /// 전체 채널 강제 체크 예정 시각
    full_refresh_at: Mutex<DateTime<Utc>>,
}

impl TieredScheduler {
    /// 새 스케줄러 생성 — full_refresh_at은 즉시 만료(epoch)로 초기화
    pub fn new() -> Self {
        Self {
            states: DashMap::new(),
            // epoch으로 초기화하면 첫 호출 시 즉시 full refresh 트리거
            full_refresh_at: Mutex::new(DateTime::UNIX_EPOCH),
        }
    }

    // ── 공개 메서드 ──────────────────────────────────────────────────────────

    /// 체크 대상 채널 목록 반환
    /// full_refresh_at이 지나면 모든 채널을 반환하고 타이머를 재설정한다.
    pub fn select_due_channels(&self, channel_ids: &[String]) -> Vec<String> {
        let now = Utc::now();

        // full_refresh 만료 여부 확인 후 즉시 락 해제
        let force_all = {
            let mut refresh = self.full_refresh_at.lock();
            if now >= *refresh {
                *refresh = now + chrono::Duration::from_std(FULL_REFRESH_INTERVAL).unwrap();
                true
            } else {
                false
            }
        };

        if force_all {
            return channel_ids.to_vec();
        }

        let mut due = Vec::with_capacity(channel_ids.len());
        for id in channel_ids {
            let is_due = match self.states.get(id) {
                // 상태 없음 → 미체크 채널 → 즉시 due
                None => true,
                Some(st) => {
                    // force_due 플래그 or 체크 시각 도달
                    st.force_due || now >= st.next_check_at
                }
            };
            if is_due {
                due.push(id.clone());
            }
        }

        tracing::debug!(
            all_channels = channel_ids.len(),
            due_channels = due.len(),
            "Tier gating applied"
        );

        due
    }

    /// 방송 시작까지 남은 시간으로 다음 체크 시각을 계산한다.
    pub fn compute_next_check_at(
        &self,
        nearest_start: Option<DateTime<Utc>>,
        last_notified_at: Option<DateTime<Utc>>,
    ) -> DateTime<Utc> {
        let now = Utc::now();

        let Some(start) = nearest_start else {
            // 예정 없음: 최근 알림 발송 채널은 고빈도 폴링 유지
            if let Some(notified) = last_notified_at {
                let elapsed = now - notified;
                let window = chrono::Duration::from_std(RECENTLY_NOTIFIED_WINDOW).unwrap();
                if elapsed <= window {
                    return now + chrono::Duration::from_std(TIER2_INTERVAL).unwrap();
                }
            }
            return now + chrono::Duration::from_std(NO_UPCOMING_INTERVAL).unwrap();
        };

        let time_to_start = start - now;

        // 이미 지남 또는 현재 → Tier1
        if time_to_start <= chrono::Duration::zero() {
            return now + chrono::Duration::from_std(TIER1_INTERVAL).unwrap();
        }

        let tier1 = chrono::Duration::from_std(TIER1_WINDOW).unwrap();
        let tier2 = chrono::Duration::from_std(TIER2_WINDOW).unwrap();
        let tier3 = chrono::Duration::from_std(TIER3_WINDOW).unwrap();

        if time_to_start <= tier1 {
            now + chrono::Duration::from_std(TIER1_INTERVAL).unwrap()
        } else if time_to_start <= tier2 {
            now + chrono::Duration::from_std(TIER2_INTERVAL).unwrap()
        } else if time_to_start <= tier3 {
            now + chrono::Duration::from_std(TIER3_INTERVAL).unwrap()
        } else {
            now + chrono::Duration::from_std(TIER4_INTERVAL).unwrap()
        }
    }

    /// 스트림 목록을 기반으로 채널 상태를 갱신한다.
    pub fn update_channel_state(&self, channel_id: &str, streams: &[Stream]) {
        let now = Utc::now();

        // 미래 예정 방송 중 가장 가까운 시작 시각 탐색
        let nearest = streams
            .iter()
            .filter(|s| s.is_upcoming())
            .filter_map(|s| s.start_scheduled)
            .filter(|&t| t > now)
            .min();

        // 기존 last_notified_at 보존 (고빈도 폴링 윈도우 유지)
        let last_notified_at = self
            .states
            .get(channel_id)
            .and_then(|st| st.last_notified_at);

        let next_at = self.compute_next_check_at(nearest, last_notified_at);

        self.states
            .entry(channel_id.to_string())
            .and_modify(|st| {
                st.last_checked_at = now;
                st.next_check_at = next_at;
                st.last_streams_count = streams.len();
                st.force_due = false;
                st.nearest_start_at = nearest;
            })
            .or_insert_with(|| ChannelScheduleState {
                next_check_at: next_at,
                last_checked_at: now,
                nearest_start_at: nearest,
                force_due: false,
                last_streams_count: streams.len(),
                last_notified_at,
            });

        tracing::debug!(
            channel_id,
            ?next_at,
            has_nearest = nearest.is_some(),
            streams = streams.len(),
            "Channel schedule state updated"
        );
    }

    /// 채널을 즉시 체크 대상으로 표시한다.
    pub fn mark_channel_due(&self, channel_id: &str) {
        let now = Utc::now();
        self.states
            .entry(channel_id.to_string())
            .and_modify(|st| {
                st.force_due = true;
                st.next_check_at = now;
            })
            .or_insert_with(|| ChannelScheduleState {
                next_check_at: now,
                last_checked_at: DateTime::UNIX_EPOCH,
                nearest_start_at: None,
                force_due: true,
                last_streams_count: 0,
                last_notified_at: None,
            });

        tracing::debug!(channel_id, "Channel marked due");
    }

    /// 알림 발송 성공 후 채널의 고빈도 폴링 윈도우를 갱신한다.
    pub fn mark_channel_recently_notified(&self, channel_id: &str) {
        let now = Utc::now();
        self.states
            .entry(channel_id.to_string())
            .and_modify(|st| {
                st.last_notified_at = Some(now);
            })
            .or_insert_with(|| ChannelScheduleState {
                next_check_at: now,
                last_checked_at: DateTime::UNIX_EPOCH,
                nearest_start_at: None,
                force_due: false,
                last_streams_count: 0,
                last_notified_at: Some(now),
            });
    }

    /// 채널 상태를 삭제하여 스케줄러에서 제거한다.
    pub fn forget_channel(&self, channel_id: &str) {
        self.states.remove(channel_id);
        tracing::debug!(channel_id, "Channel schedule state forgotten");
    }
}

impl Default for TieredScheduler {
    fn default() -> Self {
        Self::new()
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use alarm_core::{
        constants::{
            NO_UPCOMING_INTERVAL, TIER1_INTERVAL, TIER1_WINDOW, TIER2_INTERVAL, TIER2_WINDOW,
            TIER3_INTERVAL, TIER3_WINDOW, TIER4_INTERVAL,
        },
        model::{Channel, Stream, StreamStatus},
    };

    use super::*;

    // ── 헬퍼 ─────────────────────────────────────────────────────────────────

    fn make_stream(status: StreamStatus, start: Option<DateTime<Utc>>) -> Stream {
        Stream {
            id: "vid".into(),
            title: "test".into(),
            channel_id: "UC_test".into(),
            channel_name: "Tester".into(),
            status,
            start_scheduled: start,
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: None,
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: String::new(),
            is_integrated: false,
            is_chzzk_only: false,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: String::new(),
            is_twitch_only: false,
        }
    }

    fn make_channel() -> Channel {
        Channel {
            id: "UC_test".into(),
            name: "Tester".into(),
            english_name: None,
            photo: None,
            twitter: None,
            video_count: None,
            subscriber_count: None,
            org: None,
            suborg: None,
            group: None,
        }
    }

    /// Duration을 chrono::Duration으로 변환하는 헬퍼
    fn std_to_chrono(d: std::time::Duration) -> chrono::Duration {
        chrono::Duration::from_std(d).unwrap()
    }

    // ── compute_next_check_at 테스트 ─────────────────────────────────────────

    /// 예정 없음, 최근 알림 없음 → NO_UPCOMING_INTERVAL
    #[test]
    fn compute_no_upcoming_no_recent_notification() {
        let sched = TieredScheduler::new();
        let result = sched.compute_next_check_at(None, None);
        let expected = Utc::now() + std_to_chrono(NO_UPCOMING_INTERVAL);
        let diff = (result - expected).abs();
        // 1초 오차 허용
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// 예정 없음, 5분 전 알림 발송 → TIER2_INTERVAL (recently notified 윈도우 내)
    #[test]
    fn compute_no_upcoming_recently_notified_5min_ago() {
        let sched = TieredScheduler::new();
        let notified = Utc::now() - chrono::Duration::minutes(5);
        let result = sched.compute_next_check_at(None, Some(notified));
        let expected = Utc::now() + std_to_chrono(TIER2_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// 예정 없음, 20분 전 알림 발송 → RECENTLY_NOTIFIED_WINDOW(15분) 경과 → NO_UPCOMING_INTERVAL
    #[test]
    fn compute_no_upcoming_expired_notification_20min_ago() {
        let sched = TieredScheduler::new();
        let notified = Utc::now() - chrono::Duration::minutes(20);
        let result = sched.compute_next_check_at(None, Some(notified));
        let expected = Utc::now() + std_to_chrono(NO_UPCOMING_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// 이미 지난 시작 시각 → Tier1
    #[test]
    fn compute_past_start_returns_tier1() {
        let sched = TieredScheduler::new();
        let past = Utc::now() - chrono::Duration::minutes(5);
        let result = sched.compute_next_check_at(Some(past), None);
        let expected = Utc::now() + std_to_chrono(TIER1_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// Tier1: TIER1_WINDOW 이내 예정 → TIER1_INTERVAL
    #[test]
    fn compute_tier1_boundary() {
        let sched = TieredScheduler::new();
        // TIER1_WINDOW(45분)의 절반: 22분 후
        let start = Utc::now() + chrono::Duration::minutes(22);
        let result = sched.compute_next_check_at(Some(start), None);
        let expected = Utc::now() + std_to_chrono(TIER1_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// Tier2: TIER1_WINDOW 초과, TIER2_WINDOW 이내 → TIER2_INTERVAL
    #[test]
    fn compute_tier2_boundary() {
        let sched = TieredScheduler::new();
        // TIER1_WINDOW(45분) + 10분 = 55분 후
        let start = Utc::now() + std_to_chrono(TIER1_WINDOW) + chrono::Duration::minutes(10);
        let result = sched.compute_next_check_at(Some(start), None);
        let expected = Utc::now() + std_to_chrono(TIER2_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// Tier3: TIER2_WINDOW 초과, TIER3_WINDOW 이내 → TIER3_INTERVAL
    #[test]
    fn compute_tier3_boundary() {
        let sched = TieredScheduler::new();
        // TIER2_WINDOW(3h) + 1h = 4h 후
        let start = Utc::now() + std_to_chrono(TIER2_WINDOW) + chrono::Duration::hours(1);
        let result = sched.compute_next_check_at(Some(start), None);
        let expected = Utc::now() + std_to_chrono(TIER3_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    /// Tier4: TIER3_WINDOW 초과 → TIER4_INTERVAL
    #[test]
    fn compute_tier4_boundary() {
        let sched = TieredScheduler::new();
        // TIER3_WINDOW(12h) + 1h = 13h 후
        let start = Utc::now() + std_to_chrono(TIER3_WINDOW) + chrono::Duration::hours(1);
        let result = sched.compute_next_check_at(Some(start), None);
        let expected = Utc::now() + std_to_chrono(TIER4_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    // ── select_due_channels 테스트 ───────────────────────────────────────────

    /// 미등록 채널은 항상 due
    #[test]
    fn select_due_unknown_channels_are_due() {
        let sched = TieredScheduler::new();
        // full_refresh_at을 미래로 설정하여 force_all을 방지
        {
            let mut r = sched.full_refresh_at.lock();
            *r = Utc::now() + chrono::Duration::hours(1);
        }
        let ids = vec!["UC_A".to_string(), "UC_B".to_string()];
        let due = sched.select_due_channels(&ids);
        assert_eq!(due.len(), 2);
    }

    /// force_due 채널은 항상 due
    #[test]
    fn select_due_force_due_channels_are_due() {
        let sched = TieredScheduler::new();
        {
            let mut r = sched.full_refresh_at.lock();
            *r = Utc::now() + chrono::Duration::hours(1);
        }
        let id = "UC_A".to_string();
        // 미래 next_check_at으로 등록 후 force_due 설정
        sched.states.insert(
            id.clone(),
            ChannelScheduleState {
                next_check_at: Utc::now() + chrono::Duration::hours(1),
                last_checked_at: Utc::now(),
                nearest_start_at: None,
                force_due: true,
                last_streams_count: 0,
                last_notified_at: None,
            },
        );
        let due = sched.select_due_channels(&[id]);
        assert_eq!(due.len(), 1);
    }

    /// next_check_at이 미래인 채널은 due가 아님
    #[test]
    fn select_due_future_next_check_at_not_due() {
        let sched = TieredScheduler::new();
        {
            let mut r = sched.full_refresh_at.lock();
            *r = Utc::now() + chrono::Duration::hours(1);
        }
        let id = "UC_A".to_string();
        sched.states.insert(
            id.clone(),
            ChannelScheduleState {
                next_check_at: Utc::now() + chrono::Duration::hours(1),
                last_checked_at: Utc::now(),
                nearest_start_at: None,
                force_due: false,
                last_streams_count: 0,
                last_notified_at: None,
            },
        );
        let due = sched.select_due_channels(&[id]);
        assert!(due.is_empty());
    }

    // ── update_channel_state 테스트 ──────────────────────────────────────────

    /// update_channel_state: 스트림 목록으로 nearest_start 갱신
    #[test]
    fn update_channel_state_sets_nearest_start() {
        let sched = TieredScheduler::new();
        let start = Utc::now() + chrono::Duration::hours(2);
        let stream = make_stream(StreamStatus::Upcoming, Some(start));
        let _ = make_channel(); // 사용하지 않음 (컴파일 검증)
        sched.update_channel_state("UC_A", &[stream]);
        let st = sched.states.get("UC_A").unwrap();
        assert!(st.nearest_start_at.is_some());
        let diff = (st.nearest_start_at.unwrap() - start).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    // ── mark_channel_due 테스트 ──────────────────────────────────────────────

    /// mark_channel_due 후 select_due에서 반환됨
    #[test]
    fn mark_channel_due_then_select_returns_it() {
        let sched = TieredScheduler::new();
        {
            let mut r = sched.full_refresh_at.lock();
            *r = Utc::now() + chrono::Duration::hours(1);
        }
        let id = "UC_A".to_string();
        // 미래 next_check_at으로 등록
        sched.states.insert(
            id.clone(),
            ChannelScheduleState {
                next_check_at: Utc::now() + chrono::Duration::hours(1),
                last_checked_at: Utc::now(),
                nearest_start_at: None,
                force_due: false,
                last_streams_count: 0,
                last_notified_at: None,
            },
        );
        // mark_channel_due 호출 전에는 due가 아님
        assert!(
            sched
                .select_due_channels(std::slice::from_ref(&id))
                .is_empty()
        );
        // mark 후 due
        sched.mark_channel_due(&id);
        let due = sched.select_due_channels(&[id]);
        assert_eq!(due.len(), 1);
    }

    // ── mark_channel_recently_notified 테스트 ───────────────────────────────

    /// mark_channel_recently_notified 후 compute_next_check_at에서 TIER2_INTERVAL 반영
    #[test]
    fn mark_recently_notified_affects_compute() {
        let sched = TieredScheduler::new();
        let id = "UC_A";
        sched.mark_channel_recently_notified(id);

        let st = sched.states.get(id).unwrap();
        let last_notified = st.last_notified_at;
        drop(st);

        // 예정 없음 + 최근 알림 → TIER2_INTERVAL
        let result = sched.compute_next_check_at(None, last_notified);
        let expected = Utc::now() + std_to_chrono(TIER2_INTERVAL);
        let diff = (result - expected).abs();
        assert!(diff < chrono::Duration::seconds(1));
    }

    // ── full_refresh 테스트 ──────────────────────────────────────────────────

    /// full_refresh_at이 지났을 때 모든 채널 반환
    #[test]
    fn full_refresh_returns_all_channels_when_expired() {
        let sched = TieredScheduler::new();
        // full_refresh_at을 과거로 설정 (epoch이 기본값이므로 이미 만료)
        let ids: Vec<String> = vec!["UC_A".to_string(), "UC_B".to_string(), "UC_C".to_string()];
        // 모든 채널에 미래 next_check_at 설정 (due가 아님)
        for id in &ids {
            sched.states.insert(
                id.clone(),
                ChannelScheduleState {
                    next_check_at: Utc::now() + chrono::Duration::hours(1),
                    last_checked_at: Utc::now(),
                    nearest_start_at: None,
                    force_due: false,
                    last_streams_count: 0,
                    last_notified_at: None,
                },
            );
        }
        // full_refresh_at이 만료(epoch)이므로 모두 반환
        let due = sched.select_due_channels(&ids);
        assert_eq!(due.len(), 3);
    }
}
