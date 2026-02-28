use std::{
    collections::HashMap,
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
    time::{Duration, Instant},
};

use alarm_core::{error::AlarmError, keys::ALARM_CHANNEL_REGISTRY_KEY};
use alarm_infra::valkey::ValkeyClient;
use metrics::{counter, histogram};
use tokio::select;
use tokio_util::sync::CancellationToken;
use tracing::{info, warn};

use super::{
    checker::YouTubeChecker, chzzk_checker::ChzzkChecker, notifier::Notifier,
    twitch_checker::TwitchChecker,
};

// ─────────────────────────────────────────────────────────────────────────────
// Valkey 채널 매핑 키 상수 (Go alarm_types.go 대응)
// ─────────────────────────────────────────────────────────────────────────────

/// Valkey 해시 키: youtube_channel_id → chzzk_channel_id
const CHZZK_CHANNEL_MAP_KEY: &str = "alarm:chzzk_channels";

/// Valkey 해시 키: twitch_user_login → youtube_channel_id
const TWITCH_LOGIN_MAP_KEY: &str = "alarm:twitch_logins";

/// Valkey 해시 키: youtube_channel_id → 구독자 room_id 목록(쉼표 구분)은 SMEMBERS로 조회
const CHANNEL_SUBSCRIBERS_KEY_PREFIX: &str = "alarm:channel_subscribers:";

/// 루프 헬스 판정 시 허용할 추가 지연(스케줄링/일시 부하 버퍼)
const LOOP_HEALTH_GRACE_SECS: u64 = 15;

/// Prometheus metric: 루프 1회 실행 duration(초)
const METRIC_LOOP_DURATION_SECONDS: &str = "alarm_scheduler_loop_duration_seconds";
/// Prometheus metric: 루프 오류 누적 카운터
const METRIC_LOOP_ERRORS_TOTAL: &str = "alarm_scheduler_loop_errors_total";

#[derive(Debug, Clone, Copy)]
enum LoopRunResult {
    Ok,
    Error,
    Timeout,
}

impl LoopRunResult {
    fn as_str(self) -> &'static str {
        match self {
            Self::Ok => "ok",
            Self::Error => "error",
            Self::Timeout => "timeout",
        }
    }
}

/// 현재 epoch millisecond
fn now_ms() -> u64 {
    use std::time::{SystemTime, UNIX_EPOCH};

    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis())
        .unwrap_or(0)
        .min(u128::from(u64::MAX)) as u64
}

fn duration_to_ms(duration: Duration) -> u64 {
    duration.as_millis().min(u128::from(u64::MAX)) as u64
}

/// 루프별 최근 heartbeat 기반 런타임 헬스 상태
#[derive(Debug)]
pub struct SchedulerRuntimeHealth {
    youtube_last_beat_ms: AtomicU64,
    chzzk_last_beat_ms: AtomicU64,
    twitch_last_beat_ms: AtomicU64,
    twitch_enabled: bool,
    youtube_stale_after_ms: u64,
    chzzk_stale_after_ms: u64,
    twitch_stale_after_ms: u64,
}

/// 스케줄러 루프 헬스 스냅샷
#[derive(Debug, Clone, Copy)]
pub struct SchedulerHealthSnapshot {
    pub youtube_healthy: bool,
    pub chzzk_healthy: bool,
    pub twitch_healthy: bool,
}

impl SchedulerHealthSnapshot {
    pub fn overall_healthy(self) -> bool {
        self.youtube_healthy && self.chzzk_healthy && self.twitch_healthy
    }
}

impl SchedulerRuntimeHealth {
    fn new(
        youtube_stale_after: Duration,
        chzzk_stale_after: Duration,
        twitch_stale_after: Duration,
        twitch_enabled: bool,
    ) -> Self {
        let now = now_ms();
        Self {
            youtube_last_beat_ms: AtomicU64::new(now),
            chzzk_last_beat_ms: AtomicU64::new(now),
            twitch_last_beat_ms: AtomicU64::new(now),
            twitch_enabled,
            youtube_stale_after_ms: duration_to_ms(youtube_stale_after),
            chzzk_stale_after_ms: duration_to_ms(chzzk_stale_after),
            twitch_stale_after_ms: duration_to_ms(twitch_stale_after),
        }
    }

    fn mark_youtube_beat(&self) {
        self.youtube_last_beat_ms.store(now_ms(), Ordering::Relaxed);
    }

    fn mark_chzzk_beat(&self) {
        self.chzzk_last_beat_ms.store(now_ms(), Ordering::Relaxed);
    }

    fn mark_twitch_beat(&self) {
        self.twitch_last_beat_ms.store(now_ms(), Ordering::Relaxed);
    }

    fn mark_all_beats(&self) {
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

// ─────────────────────────────────────────────────────────────────────────────
// AlarmScheduler: 3개 독립 루프 조율 (YouTube / Chzzk / Twitch)
// ─────────────────────────────────────────────────────────────────────────────

/// 알람 스케줄러 — YouTube, Chzzk, Twitch 루프를 독립적으로 실행한다.
pub struct AlarmScheduler {
    youtube_checker: Arc<YouTubeChecker>,
    chzzk_checker: Arc<ChzzkChecker>,
    twitch_checker: Arc<TwitchChecker>,
    notifier: Arc<Notifier>,
    valkey: Arc<dyn ValkeyClient>,
    twitch_enabled: bool,
    chzzk_poll_interval: Duration,
    twitch_poll_interval: Duration,
    youtube_check_timeout: Duration,
    chzzk_check_timeout: Duration,
    twitch_check_timeout: Duration,
    runtime_health: Arc<SchedulerRuntimeHealth>,
}

#[derive(Debug, Clone, Copy)]
pub struct SchedulerTimingConfig {
    pub twitch_enabled: bool,
    pub chzzk_poll_secs: u64,
    pub twitch_poll_secs: u64,
    pub youtube_check_timeout_secs: u64,
    pub chzzk_check_timeout_secs: u64,
    pub twitch_check_timeout_secs: u64,
}

impl AlarmScheduler {
    /// AlarmScheduler 생성
    pub fn new(
        youtube_checker: Arc<YouTubeChecker>,
        chzzk_checker: Arc<ChzzkChecker>,
        twitch_checker: Arc<TwitchChecker>,
        notifier: Arc<Notifier>,
        valkey: Arc<dyn ValkeyClient>,
        timing: SchedulerTimingConfig,
    ) -> Self {
        let chzzk_poll_interval = Duration::from_secs(timing.chzzk_poll_secs);
        let twitch_poll_interval = Duration::from_secs(timing.twitch_poll_secs);
        let youtube_check_timeout = Duration::from_secs(timing.youtube_check_timeout_secs);
        let chzzk_check_timeout = Duration::from_secs(timing.chzzk_check_timeout_secs);
        let twitch_check_timeout = Duration::from_secs(timing.twitch_check_timeout_secs);
        let health_grace = Duration::from_secs(LOOP_HEALTH_GRACE_SECS);

        let runtime_health = Arc::new(SchedulerRuntimeHealth::new(
            Duration::from_secs(60)
                .saturating_add(youtube_check_timeout)
                .saturating_add(health_grace),
            chzzk_poll_interval
                .saturating_add(chzzk_check_timeout)
                .saturating_add(health_grace),
            twitch_poll_interval
                .saturating_add(twitch_check_timeout)
                .saturating_add(health_grace),
            timing.twitch_enabled,
        ));

        Self {
            youtube_checker,
            chzzk_checker,
            twitch_checker,
            notifier,
            valkey,
            twitch_enabled: timing.twitch_enabled,
            chzzk_poll_interval,
            twitch_poll_interval,
            youtube_check_timeout,
            chzzk_check_timeout,
            twitch_check_timeout,
            runtime_health,
        }
    }

    /// readiness 경로에서 사용할 런타임 헬스 핸들
    pub fn runtime_health_handle(&self) -> Arc<SchedulerRuntimeHealth> {
        Arc::clone(&self.runtime_health)
    }

    // ── 공개 진입점 ──────────────────────────────────────────────────────────

    /// 3개 루프를 spawning하여 병렬 실행하고, 토큰 취소 시 모두 종료 대기
    pub async fn run(self: Arc<Self>, token: CancellationToken) -> Result<(), AlarmError> {
        info!("알람 스케줄러 시작");
        self.runtime_health.mark_all_beats();

        let yt_token = token.child_token();
        let chzzk_token = token.child_token();
        let twitch_token = token.child_token();

        let this_yt = Arc::clone(&self);
        let yt_handle = tokio::spawn(async move { this_yt.run_youtube_loop(yt_token).await });

        let this_chzzk = Arc::clone(&self);
        let chzzk_handle =
            tokio::spawn(async move { this_chzzk.run_chzzk_loop(chzzk_token).await });

        let this_twitch = Arc::clone(&self);
        let twitch_handle =
            tokio::spawn(async move { this_twitch.run_twitch_loop(twitch_token).await });

        // 취소 신호 대기 후 모든 루프 종료 확인
        token.cancelled().await;
        info!("알람 스케줄러 종료 신호 수신 — 루프 대기 중");

        let _ = yt_handle.await;
        let _ = chzzk_handle.await;
        let _ = twitch_handle.await;

        info!("알람 스케줄러 종료 완료");
        Ok(())
    }

    // ── 루프 구현 ─────────────────────────────────────────────────────────────

    /// YouTube 루프 — 1분 기본 틱마다 TieredScheduler 기반 체크
    async fn run_youtube_loop(&self, token: CancellationToken) {
        loop {
            select! {
                _ = token.cancelled() => {
                    info!("YouTube 루프 종료");
                    break;
                }
                _ = tokio::time::sleep(Duration::from_secs(60)) => {
                    let started_at = Instant::now();
                    let result = match tokio::time::timeout(self.youtube_check_timeout, self.run_youtube_iteration()).await {
                        Ok(outcome) => outcome,
                        Err(_) => {
                            self.record_loop_error("youtube", "timeout");
                            warn!(
                            timeout_secs = self.youtube_check_timeout.as_secs(),
                            "YouTube 체크 타임아웃"
                        );
                            LoopRunResult::Timeout
                        }
                    };
                    self.record_loop_duration("youtube", result, started_at.elapsed());
                    self.runtime_health.mark_youtube_beat();
                }
            }
        }
    }

    /// Chzzk 루프 — 설정된 고정 간격으로 체크
    async fn run_chzzk_loop(&self, token: CancellationToken) {
        loop {
            select! {
                _ = token.cancelled() => {
                    info!("Chzzk 루프 종료");
                    break;
                }
                _ = tokio::time::sleep(self.chzzk_poll_interval) => {
                    let started_at = Instant::now();
                    let result = match tokio::time::timeout(self.chzzk_check_timeout, self.run_chzzk_iteration()).await {
                        Ok(outcome) => outcome,
                        Err(_) => {
                            self.record_loop_error("chzzk", "timeout");
                            warn!(
                            timeout_secs = self.chzzk_check_timeout.as_secs(),
                            "Chzzk 체크 타임아웃"
                        );
                            LoopRunResult::Timeout
                        }
                    };
                    self.record_loop_duration("chzzk", result, started_at.elapsed());
                    self.runtime_health.mark_chzzk_beat();
                }
            }
        }
    }

    /// Twitch 루프 — 설정된 고정 간격으로 체크
    async fn run_twitch_loop(&self, token: CancellationToken) {
        if !self.twitch_enabled {
            info!("Twitch 루프 비활성화 — 체크를 건너뜁니다");
            token.cancelled().await;
            info!("Twitch 루프 종료");
            return;
        }

        loop {
            select! {
                _ = token.cancelled() => {
                    info!("Twitch 루프 종료");
                    break;
                }
                _ = tokio::time::sleep(self.twitch_poll_interval) => {
                    let started_at = Instant::now();
                    let result = match tokio::time::timeout(self.twitch_check_timeout, self.run_twitch_iteration()).await {
                        Ok(outcome) => outcome,
                        Err(_) => {
                            self.record_loop_error("twitch", "timeout");
                            warn!(
                            timeout_secs = self.twitch_check_timeout.as_secs(),
                            "Twitch 체크 타임아웃"
                        );
                            LoopRunResult::Timeout
                        }
                    };
                    self.record_loop_duration("twitch", result, started_at.elapsed());
                    self.runtime_health.mark_twitch_beat();
                }
            }
        }
    }

    fn record_loop_duration(
        &self,
        loop_name: &'static str,
        result: LoopRunResult,
        elapsed: Duration,
    ) {
        histogram!(
            METRIC_LOOP_DURATION_SECONDS,
            "loop" => loop_name,
            "result" => result.as_str()
        )
        .record(elapsed.as_secs_f64());
    }

    fn record_loop_error(&self, loop_name: &'static str, error_type: &'static str) {
        counter!(
            METRIC_LOOP_ERRORS_TOTAL,
            "loop" => loop_name,
            "error_type" => error_type
        )
        .increment(1);
    }

    async fn run_youtube_iteration(&self) -> LoopRunResult {
        match self
            .youtube_checker
            .check_upcoming_streams_with_budget(Some(self.youtube_iteration_budget()))
            .await
        {
            Ok(notifications) if !notifications.is_empty() => {
                if let Err(e) = self.notifier.send_notifications(notifications).await {
                    warn!(error = %e, "YouTube 알림 발송 실패");
                    self.record_loop_error("youtube", "notify");
                    return LoopRunResult::Error;
                }
                LoopRunResult::Ok
            }
            Ok(_) => LoopRunResult::Ok,
            Err(e) => {
                warn!(error = %e, "YouTube 체크 실패");
                self.record_loop_error("youtube", "check");
                LoopRunResult::Error
            }
        }
    }

    fn youtube_iteration_budget(&self) -> Duration {
        // timeout 직전에 취소(drop)되기 전에 checker가 스스로 종료할 수 있도록
        // 1초 여유를 둔 내부 예산을 사용한다.
        self.youtube_check_timeout
            .saturating_sub(Duration::from_secs(1))
            .max(Duration::from_secs(1))
    }

    async fn run_chzzk_iteration(&self) -> LoopRunResult {
        match self.fetch_chzzk_mappings().await {
            Ok((channel_mappings, subscriber_map)) => {
                match self
                    .chzzk_checker
                    .check_chzzk_streams(&channel_mappings, &subscriber_map)
                    .await
                {
                    Ok(notifications) if !notifications.is_empty() => {
                        if let Err(e) = self.notifier.send_notifications(notifications).await {
                            warn!(error = %e, "Chzzk 알림 발송 실패");
                            self.record_loop_error("chzzk", "notify");
                            return LoopRunResult::Error;
                        }
                        LoopRunResult::Ok
                    }
                    Ok(_) => LoopRunResult::Ok,
                    Err(e) => {
                        warn!(error = %e, "Chzzk 체크 실패");
                        self.record_loop_error("chzzk", "check");
                        LoopRunResult::Error
                    }
                }
            }
            Err(e) => {
                warn!(error = %e, "Chzzk 매핑 조회 실패");
                self.record_loop_error("chzzk", "mapping");
                LoopRunResult::Error
            }
        }
    }

    async fn run_twitch_iteration(&self) -> LoopRunResult {
        match self.fetch_twitch_mappings().await {
            Ok((login_mappings, subscriber_map)) => {
                match self
                    .twitch_checker
                    .check_twitch_streams(&login_mappings, &subscriber_map)
                    .await
                {
                    Ok(notifications) if !notifications.is_empty() => {
                        if let Err(e) = self.notifier.send_notifications(notifications).await {
                            warn!(error = %e, "Twitch 알림 발송 실패");
                            self.record_loop_error("twitch", "notify");
                            return LoopRunResult::Error;
                        }
                        LoopRunResult::Ok
                    }
                    Ok(_) => LoopRunResult::Ok,
                    Err(e) => {
                        warn!(error = %e, "Twitch 체크 실패");
                        self.record_loop_error("twitch", "check");
                        LoopRunResult::Error
                    }
                }
            }
            Err(e) => {
                warn!(error = %e, "Twitch 매핑 조회 실패");
                self.record_loop_error("twitch", "mapping");
                LoopRunResult::Error
            }
        }
    }

    // ── 매핑 조회 헬퍼 ───────────────────────────────────────────────────────

    /// Chzzk 채널 매핑 + 구독자 맵 조회
    ///
    /// 반환:
    ///   - channel_mappings: youtube_channel_id → chzzk_channel_id
    ///   - subscriber_map: youtube_channel_id → [room_id, ...]
    async fn fetch_chzzk_mappings(
        &self,
    ) -> Result<(HashMap<String, String>, HashMap<String, Vec<String>>), AlarmError> {
        // alarm:chzzk_channels 해시에서 매핑 조회 (youtube_id → chzzk_id)
        let channel_mappings = self.valkey.hget_all(CHZZK_CHANNEL_MAP_KEY).await?;

        if channel_mappings.is_empty() {
            return Ok((HashMap::new(), HashMap::new()));
        }

        // 각 youtube_channel_id의 구독자 목록 조회
        let subscriber_map = self.fetch_subscriber_map(channel_mappings.keys()).await?;
        Ok((channel_mappings, subscriber_map))
    }

    /// Twitch 로그인 매핑 + 구독자 맵 조회
    ///
    /// 반환:
    ///   - login_mappings: twitch_user_login → youtube_channel_id
    ///   - subscriber_map: youtube_channel_id → [room_id, ...]
    async fn fetch_twitch_mappings(
        &self,
    ) -> Result<(HashMap<String, String>, HashMap<String, Vec<String>>), AlarmError> {
        // alarm:twitch_logins 해시에서 매핑 조회 (login → youtube_id)
        let login_mappings = self.valkey.hget_all(TWITCH_LOGIN_MAP_KEY).await?;

        if login_mappings.is_empty() {
            return Ok((HashMap::new(), HashMap::new()));
        }

        // youtube_channel_id 목록으로 구독자 맵 조회
        let youtube_ids: Vec<String> = login_mappings.values().cloned().collect();
        let subscriber_map = self.fetch_subscriber_map(youtube_ids.iter()).await?;
        Ok((login_mappings, subscriber_map))
    }

    /// youtube_channel_id 목록 → 구독자 맵 조회
    /// alarm:channel_subscribers:{channel_id} SMEMBERS → room_id 목록
    async fn fetch_subscriber_map<'a, I>(
        &self,
        youtube_ids: I,
    ) -> Result<HashMap<String, Vec<String>>, AlarmError>
    where
        I: Iterator<Item = &'a String>,
    {
        let mut map = HashMap::new();
        let channel_ids: Vec<String> = youtube_ids.cloned().collect();
        if channel_ids.is_empty() {
            return Ok(map);
        }

        let subscriber_keys: Vec<String> = channel_ids
            .iter()
            .map(|channel_id| format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}{channel_id}"))
            .collect();

        let room_lists = self.valkey.smembers_multi(&subscriber_keys).await?;
        for (channel_id, rooms) in channel_ids.into_iter().zip(room_lists.into_iter()) {
            if !rooms.is_empty() {
                map.insert(channel_id, rooms);
            }
        }

        Ok(map)
    }

    /// 채널 레지스트리에서 모든 구독 채널 ID 조회 (디버그/헬스체크용)
    pub async fn registry_channel_count(&self) -> usize {
        self.valkey
            .smembers(ALARM_CHANNEL_REGISTRY_KEY)
            .await
            .map(|ids| ids.len())
            .unwrap_or(0)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use std::sync::atomic::AtomicUsize;

    use alarm_core::constants::DEFAULT_TARGET_MINUTES;
    use alarm_infra::{
        chzzk::MockChzzkClient,
        holodex::MockHolodexClient,
        twitch::MockTwitchClient,
        valkey::{MockValkeyClient, ValkeyClient},
    };
    use async_trait::async_trait;

    use super::*;
    use crate::{dedup::DedupService, queue::QueuePublisher, tier::TieredScheduler};

    // ── 헬퍼 ─────────────────────────────────────────────────────────────────

    fn make_scheduler_with_valkey(valkey: Arc<dyn ValkeyClient>) -> Arc<AlarmScheduler> {
        let holodex = Arc::new(MockHolodexClient::new(vec![]));
        let scheduler = Arc::new(TieredScheduler::new());
        let dedup = Arc::new(DedupService::new(
            Arc::clone(&valkey),
            DEFAULT_TARGET_MINUTES.to_vec(),
        ));

        let youtube_checker = Arc::new(crate::checker::YouTubeChecker::new(
            holodex as Arc<dyn alarm_infra::holodex::HolodexClient>,
            Arc::clone(&valkey),
            Arc::clone(&scheduler),
            Arc::clone(&dedup),
            DEFAULT_TARGET_MINUTES.to_vec(),
        ));

        let chzzk_checker = Arc::new(ChzzkChecker::new(
            Arc::new(MockChzzkClient::new(None)) as Arc<dyn alarm_infra::chzzk::ChzzkClient>,
            Arc::clone(&valkey),
        ));

        let twitch_checker = Arc::new(TwitchChecker::new(
            Arc::new(MockTwitchClient::new(vec![])) as Arc<dyn alarm_infra::twitch::TwitchClient>,
            Arc::clone(&valkey),
        ));

        let queue = Arc::new(QueuePublisher::new(Arc::clone(&valkey)));
        let notifier = Arc::new(Notifier::new(queue, dedup, scheduler));

        Arc::new(AlarmScheduler::new(
            youtube_checker,
            chzzk_checker,
            twitch_checker,
            notifier,
            Arc::clone(&valkey),
            SchedulerTimingConfig {
                twitch_enabled: true,
                chzzk_poll_secs: 120,
                twitch_poll_secs: 120,
                youtube_check_timeout_secs: 45,
                chzzk_check_timeout_secs: 30,
                twitch_check_timeout_secs: 30,
            },
        ))
    }

    fn make_scheduler(valkey: Arc<MockValkeyClient>) -> Arc<AlarmScheduler> {
        make_scheduler_with_valkey(valkey as Arc<dyn ValkeyClient>)
    }

    struct TrackingSmembersValkey {
        set_data: HashMap<String, Vec<String>>,
        smembers_calls: AtomicUsize,
        smembers_multi_calls: AtomicUsize,
    }

    impl TrackingSmembersValkey {
        fn new(set_data: HashMap<String, Vec<String>>) -> Self {
            Self {
                set_data,
                smembers_calls: AtomicUsize::new(0),
                smembers_multi_calls: AtomicUsize::new(0),
            }
        }
    }

    #[async_trait]
    impl ValkeyClient for TrackingSmembersValkey {
        async fn get(&self, _: &str) -> Result<Option<String>, AlarmError> {
            Ok(None)
        }

        async fn set(&self, _: &str, _: &str, _: Duration) -> Result<(), AlarmError> {
            Ok(())
        }

        async fn set_nx(&self, _: &str, _: &str, _: Duration) -> Result<bool, AlarmError> {
            Ok(true)
        }

        async fn del(&self, _: &[&str]) -> Result<u64, AlarmError> {
            Ok(0)
        }

        async fn hget(&self, _: &str, _: &str) -> Result<Option<String>, AlarmError> {
            Ok(None)
        }

        async fn hset(&self, _: &str, _: &str, _: &str) -> Result<(), AlarmError> {
            Ok(())
        }

        async fn hget_all(&self, _: &str) -> Result<HashMap<String, String>, AlarmError> {
            Ok(HashMap::new())
        }

        async fn hmset(&self, _: &str, _: &HashMap<String, String>) -> Result<(), AlarmError> {
            Ok(())
        }

        async fn smembers(&self, key: &str) -> Result<Vec<String>, AlarmError> {
            self.smembers_calls.fetch_add(1, Ordering::Relaxed);
            Ok(self.set_data.get(key).cloned().unwrap_or_default())
        }

        async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, AlarmError> {
            self.smembers_multi_calls.fetch_add(1, Ordering::Relaxed);
            Ok(keys
                .iter()
                .map(|key| self.set_data.get(key).cloned().unwrap_or_default())
                .collect())
        }

        async fn expire(&self, _: &str, _: Duration) -> Result<bool, AlarmError> {
            Ok(false)
        }

        async fn lpush(&self, _: &str, _: &str) -> Result<i64, AlarmError> {
            Ok(0)
        }

        async fn ping(&self) -> Result<(), AlarmError> {
            Ok(())
        }
    }

    // ── 생성 테스트 ──────────────────────────────────────────────────────────

    /// AlarmScheduler 생성 확인
    #[test]
    fn scheduler_can_be_constructed() {
        let valkey = Arc::new(MockValkeyClient::new());
        let _scheduler = make_scheduler(valkey);
    }

    // ── 취소 토큰 테스트 ─────────────────────────────────────────────────────

    /// 취소 토큰이 루프를 정상 종료시키는지 확인
    /// 짧은 대기 후 취소 → join이 완료되어야 함
    #[tokio::test]
    async fn cancellation_token_stops_scheduler() {
        let valkey = Arc::new(MockValkeyClient::new());
        let scheduler = make_scheduler(valkey);

        let token = CancellationToken::new();
        let token_clone = token.clone();
        let sched_clone = Arc::clone(&scheduler);

        // 스케줄러를 별도 태스크에서 실행
        let handle = tokio::spawn(async move { sched_clone.run(token_clone).await });

        // 즉시 취소 (루프 진입 전 취소 가능하므로 짧은 sleep 없이도 동작)
        token.cancel();

        // 취소 후 join이 완료되어야 함 (타임아웃 내)
        let result = tokio::time::timeout(Duration::from_secs(5), handle).await;

        assert!(result.is_ok(), "스케줄러가 5초 내 종료되어야 함");
        let join_result = result.unwrap();
        assert!(
            join_result.is_ok(),
            "스케줄러 태스크가 패닉 없이 종료되어야 함"
        );
    }

    #[test]
    fn runtime_health_snapshot_is_healthy_after_recent_heartbeat() {
        let health = SchedulerRuntimeHealth::new(
            Duration::from_millis(200),
            Duration::from_millis(200),
            Duration::from_millis(200),
            true,
        );
        health.mark_all_beats();

        let snapshot = health.snapshot();
        assert!(snapshot.youtube_healthy);
        assert!(snapshot.chzzk_healthy);
        assert!(snapshot.twitch_healthy);
        assert!(snapshot.overall_healthy());
    }

    #[test]
    fn runtime_health_snapshot_marks_stale_loops_unhealthy() {
        let health = SchedulerRuntimeHealth::new(
            Duration::from_millis(20),
            Duration::from_millis(20),
            Duration::from_millis(20),
            true,
        );
        health.mark_all_beats();
        std::thread::sleep(Duration::from_millis(50));

        let snapshot = health.snapshot();
        assert!(!snapshot.youtube_healthy);
        assert!(!snapshot.chzzk_healthy);
        assert!(!snapshot.twitch_healthy);
        assert!(!snapshot.overall_healthy());
    }

    #[test]
    fn runtime_health_snapshot_ignores_twitch_when_disabled() {
        let health = SchedulerRuntimeHealth::new(
            Duration::from_millis(200),
            Duration::from_millis(200),
            Duration::from_millis(20),
            false,
        );
        health.mark_all_beats();
        std::thread::sleep(Duration::from_millis(50));

        let snapshot = health.snapshot();
        assert!(snapshot.youtube_healthy);
        assert!(snapshot.chzzk_healthy);
        assert!(snapshot.twitch_healthy);
        assert!(snapshot.overall_healthy());
    }

    #[tokio::test]
    async fn fetch_subscriber_map_uses_smembers_multi_path() {
        let valkey = Arc::new(TrackingSmembersValkey::new(HashMap::from([
            (
                format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}UC_A"),
                vec!["room1".to_string()],
            ),
            (
                format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}UC_B"),
                vec!["room2".to_string(), "room3".to_string()],
            ),
        ])));
        let scheduler = make_scheduler_with_valkey(Arc::clone(&valkey) as Arc<dyn ValkeyClient>);

        let channel_ids = [
            "UC_A".to_string(),
            "UC_B".to_string(),
            "UC_EMPTY".to_string(),
        ];
        let subscriber_map = scheduler
            .fetch_subscriber_map(channel_ids.iter())
            .await
            .unwrap();

        assert_eq!(valkey.smembers_multi_calls.load(Ordering::Relaxed), 1);
        assert_eq!(valkey.smembers_calls.load(Ordering::Relaxed), 0);
        assert_eq!(subscriber_map.get("UC_A"), Some(&vec!["room1".to_string()]));
        assert_eq!(
            subscriber_map.get("UC_B"),
            Some(&vec!["room2".to_string(), "room3".to_string()])
        );
        assert!(!subscriber_map.contains_key("UC_EMPTY"));
    }
}
