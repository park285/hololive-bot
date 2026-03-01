use std::time::{Duration, Instant};

use tokio::select;
use tokio_util::sync::CancellationToken;
use tracing::{info, warn};

use super::{AlarmScheduler, LoopRunResult};

impl AlarmScheduler {
    /// YouTube 루프 — 1분 기본 틱마다 TieredScheduler 기반 체크
    pub(super) async fn run_youtube_loop(&self, token: CancellationToken) {
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
    pub(super) async fn run_chzzk_loop(&self, token: CancellationToken) {
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
    pub(super) async fn run_twitch_loop(&self, token: CancellationToken) {
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

    pub(super) async fn run_youtube_iteration(&self) -> LoopRunResult {
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

    pub(super) fn youtube_iteration_budget(&self) -> Duration {
        // timeout 직전에 취소(drop)되기 전에 checker가 스스로 종료할 수 있도록
        // 1초 여유를 둔 내부 예산을 사용한다.
        self.youtube_check_timeout
            .saturating_sub(Duration::from_secs(1))
            .max(Duration::from_secs(1))
    }

    pub(super) async fn run_chzzk_iteration(&self) -> LoopRunResult {
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

    pub(super) async fn run_twitch_iteration(&self) -> LoopRunResult {
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
}
