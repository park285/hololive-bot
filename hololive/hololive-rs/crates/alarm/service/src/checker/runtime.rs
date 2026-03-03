use std::{
    collections::HashMap,
    time::{Duration, Instant},
};

use alarm_core::{
    error::AlarmError,
    keys::{ALARM_CHANNEL_REGISTRY_KEY, CHANNEL_SUBSCRIBERS_KEY_PREFIX},
    model::{AlarmNotification, Stream},
};
use chrono::Utc;
use tracing::{debug, warn};

use super::YouTubeChecker;

impl YouTubeChecker {
    /// 체크 루프 예산(시간 제한)을 적용한 upcoming/live 확인
    ///
    /// budget이 Some인 경우, 예산 초과 시 남은 채널/스트림 처리를 중단하고
    /// 현재까지 수집한 알림만 반환한다.
    pub async fn check_upcoming_streams_with_budget(
        &self,
        budget: Option<Duration>,
    ) -> Result<Vec<AlarmNotification>, AlarmError> {
        let started_at = Instant::now();

        // 1. 채널 레지스트리 조회
        let channel_ids = self
            .valkey
            .smembers(ALARM_CHANNEL_REGISTRY_KEY)
            .await
            .map_err(|e| AlarmError::Valkey(format!("채널 레지스트리 조회 실패: {e}")))?;

        if channel_ids.is_empty() {
            debug!("채널 레지스트리가 비어있음 — 스킵");
            return Ok(vec![]);
        }

        // 2. Tier 스케줄러로 체크 대상 채널 선택
        let due_channels = self.scheduler.select_due_channels(&channel_ids);
        if due_channels.is_empty() {
            debug!("체크 대상 채널 없음 — 스킵");
            return Ok(vec![]);
        }
        if Self::budget_exhausted(started_at, budget) {
            warn!(
                elapsed_ms = started_at.elapsed().as_millis(),
                "YouTube 체크 예산 초과 — Holodex 조회 전 중단"
            );
            return Ok(vec![]);
        }

        // 3. 채널별 구독자 조회 + 배치 조회
        let channel_refs: Vec<&str> = due_channels.iter().map(|s| s.as_str()).collect();
        let streams = self.holodex.get_live_streams(&channel_refs).await?;
        let streams_by_channel = Self::group_streams_by_channel(streams);

        // 4. 채널별 상태 갱신 및 알림 생성
        let mut notifications = Vec::new();
        for channel_id in &due_channels {
            if Self::budget_exhausted(started_at, budget) {
                warn!(
                    channel_id,
                    elapsed_ms = started_at.elapsed().as_millis(),
                    "YouTube 체크 예산 초과 — 남은 채널 처리 중단"
                );
                break;
            }

            // 스케줄러 상태 갱신
            let owned = streams_by_channel
                .get(channel_id)
                .cloned()
                .unwrap_or_default();
            self.scheduler.update_channel_state(channel_id, &owned);

            // 채널 구독자 조회
            let subs_key = format!("{CHANNEL_SUBSCRIBERS_KEY_PREFIX}{channel_id}");
            let subscriber_room_ids = match self.valkey.smembers(&subs_key).await {
                Ok(v) => v,
                Err(e) => {
                    warn!(channel_id, error = %e, "구독자 목록 조회 실패 — 채널 스킵");
                    continue;
                }
            };

            if subscriber_room_ids.is_empty() {
                debug!(channel_id, "구독자 없음 — 채널 스킵");
                continue;
            }

            // 예정 스트림 알림 생성
            for stream in self.filter_upcoming_streams(&owned) {
                if Self::budget_exhausted(started_at, budget) {
                    warn!(
                        channel_id,
                        stream_id = stream.id,
                        elapsed_ms = started_at.elapsed().as_millis(),
                        "YouTube 체크 예산 초과 — 예정 스트림 처리 중단"
                    );
                    break;
                }
                match self
                    .create_notification(stream, channel_id, &subscriber_room_ids)
                    .await
                {
                    Ok(mut ns) => notifications.append(&mut ns),
                    Err(e) => warn!(
                        channel_id,
                        stream_id = stream.id,
                        error = %e,
                        "예정 스트림 알림 생성 실패"
                    ),
                }
            }

            // 라이브 catch-up 알림 생성
            for stream in self.filter_live_catchup(&owned) {
                if Self::budget_exhausted(started_at, budget) {
                    warn!(
                        channel_id,
                        stream_id = stream.id,
                        elapsed_ms = started_at.elapsed().as_millis(),
                        "YouTube 체크 예산 초과 — 라이브 catch-up 처리 중단"
                    );
                    break;
                }
                match self
                    .create_live_catchup_notification(stream, channel_id, &subscriber_room_ids)
                    .await
                {
                    Ok(mut ns) => notifications.append(&mut ns),
                    Err(e) => warn!(
                        channel_id,
                        stream_id = stream.id,
                        error = %e,
                        "라이브 catch-up 알림 생성 실패"
                    ),
                }
            }
        }

        Ok(notifications)
    }

    pub(super) fn budget_exhausted(started_at: Instant, budget: Option<Duration>) -> bool {
        budget
            .map(|limit| started_at.elapsed() >= limit)
            .unwrap_or(false)
    }

    pub(super) fn group_streams_by_channel(streams: Vec<Stream>) -> HashMap<String, Vec<Stream>> {
        let mut by_channel: HashMap<String, Vec<Stream>> = HashMap::new();
        for stream in streams {
            by_channel
                .entry(stream.channel_id.clone())
                .or_default()
                .push(stream);
        }
        by_channel
    }

    /// upcoming 상태이고 start_scheduled가 현재 이후인 스트림만 반환
    pub fn filter_upcoming_streams<'a>(&self, streams: &'a [Stream]) -> Vec<&'a Stream> {
        let now = Utc::now();
        streams
            .iter()
            .filter(|s| s.is_upcoming() && s.start_scheduled.map(|t| t > now).unwrap_or(false))
            .collect()
    }

    /// live 상태이고 시작된 지 catch-up 윈도우(FULL_REFRESH_INTERVAL + 1분 = 6분) 이내인 스트림 반환
    /// start_actual 우선, 없으면 start_scheduled 사용
    pub fn filter_live_catchup<'a>(&self, streams: &'a [Stream]) -> Vec<&'a Stream> {
        let now = Utc::now();
        // catch-up 윈도우: 5분(full_refresh) + 1분 = 6분
        let catchup_window =
            chrono::Duration::from_std(alarm_core::constants::FULL_REFRESH_INTERVAL).unwrap()
                + chrono::Duration::minutes(1);

        streams
            .iter()
            .filter(|s| {
                if !s.is_live() {
                    return false;
                }
                // start_actual 우선, 없으면 start_scheduled
                let start = s.start_actual.or(s.start_scheduled);
                let Some(start_time) = start else {
                    return false;
                };
                let elapsed = now - start_time;
                // 0 이상(시작됨) + 윈도우 이내
                elapsed >= chrono::Duration::zero() && elapsed <= catchup_window
            })
            .collect()
    }
}
