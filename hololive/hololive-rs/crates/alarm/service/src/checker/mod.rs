use std::{
    collections::HashMap,
    sync::Arc,
    time::{Duration, Instant},
};

use alarm_core::{
    constants::{FULL_REFRESH_INTERVAL, LIVE_CATCHUP_SUPPRESS_WINDOW},
    error::AlarmError,
    keys::{ALARM_CHANNEL_REGISTRY_KEY, CHANNEL_SUBSCRIBERS_KEY_PREFIX},
    model::{AlarmNotification, Stream},
};
use alarm_infra::{holodex::HolodexClient, valkey::ValkeyClient};
use chrono::{DateTime, Utc};
use futures::{StreamExt, stream};
use tracing::{debug, warn};

use super::{
    checker_helpers::{format_schedule_change_message, is_target_minute, minutes_until_floor},
    dedup::DedupService,
    tier::TieredScheduler,
};

// ─────────────────────────────────────────────────────────────────────────────
// YouTubeChecker: 예정/라이브 스트림 알림 생성 메인 로직
// ─────────────────────────────────────────────────────────────────────────────

/// YouTube 스트림 알람 체커
/// Holodex를 폴링하여 예정/라이브 스트림을 감지하고 알림 목록을 생성한다.
pub struct YouTubeChecker {
    holodex: Arc<dyn HolodexClient>,
    valkey: Arc<dyn ValkeyClient>,
    scheduler: Arc<TieredScheduler>,
    dedup: Arc<DedupService>,
    /// 알림 대상 분 목록 (예: [5, 3, 1])
    target_minutes: Vec<i32>,
}

impl YouTubeChecker {
    pub fn new(
        holodex: Arc<dyn HolodexClient>,
        valkey: Arc<dyn ValkeyClient>,
        scheduler: Arc<TieredScheduler>,
        dedup: Arc<DedupService>,
        target_minutes: Vec<i32>,
    ) -> Self {
        Self {
            holodex,
            valkey,
            scheduler,
            dedup,
            target_minutes,
        }
    }

    // ── 공개 진입점 ──────────────────────────────────────────────────────────

    /// 예정/라이브 스트림을 체크하여 발송할 AlarmNotification 목록 반환
    ///
    /// Flow:
    ///   1. Valkey에서 채널 레지스트리 조회 (SMEMBERS alarm:channel_registry)
    ///   2. Tier 스케줄러로 체크 대상 채널 필터링
    ///   3. Holodex 조회 (배치 실패 폴백은 infra 계층에서 처리)
    ///   4. 채널별 상태 갱신 + 알림 생성
    pub async fn check_upcoming_streams(&self) -> Result<Vec<AlarmNotification>, AlarmError> {
        self.check_upcoming_streams_with_budget(None).await
    }

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

    fn budget_exhausted(started_at: Instant, budget: Option<Duration>) -> bool {
        budget
            .map(|limit| started_at.elapsed() >= limit)
            .unwrap_or(false)
    }

    fn group_streams_by_channel(streams: Vec<Stream>) -> HashMap<String, Vec<Stream>> {
        let mut by_channel: HashMap<String, Vec<Stream>> = HashMap::new();
        for stream in streams {
            by_channel
                .entry(stream.channel_id.clone())
                .or_default()
                .push(stream);
        }
        by_channel
    }

    // ── 필터 ─────────────────────────────────────────────────────────────────

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
        let catchup_window = chrono::Duration::from_std(FULL_REFRESH_INTERVAL).unwrap()
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

    // ── 알림 생성 ─────────────────────────────────────────────────────────────

    /// 예정 스트림에 대한 알림 생성
    ///
    /// 1. minutes_until 계산 (올림)
    /// 2. 일정 변경 감지
    /// 3. target 분도 아니고 일정 변경도 없으면 스킵
    /// 4. dedup 확인
    /// 5. 유효 구독자 검증
    /// 6. 방별 AlarmNotification 생성
    pub async fn create_notification(
        &self,
        stream: &Stream,
        channel_id: &str,
        subscriber_room_ids: &[String],
    ) -> Result<Vec<AlarmNotification>, AlarmError> {
        let Some(start_scheduled) = stream.start_scheduled else {
            return Ok(vec![]);
        };

        let now = Utc::now();
        let minutes_until = minutes_until_floor(start_scheduled, now);

        // 일정 변경 감지
        let schedule_change_msg = self.detect_schedule_change(stream).await?;
        let has_schedule_change = !schedule_change_msg.is_empty();

        // target 분도 아니고 일정 변경도 없으면 스킵
        if !has_schedule_change && !is_target_minute(&self.target_minutes, minutes_until) {
            return Ok(vec![]);
        }

        // dedup: 현재 스케줄에서 이미 발송됐으면 스킵
        let already = self
            .dedup
            .is_already_notified_for_schedule(&stream.id, start_scheduled, minutes_until)
            .await?;
        if already {
            debug!(
                stream_id = stream.id,
                minutes_until, "이미 발송된 알림 — 스킵"
            );
            return Ok(vec![]);
        }

        // 유효 구독자 검증
        let valid_rooms = self
            .validate_subscribers(channel_id, subscriber_room_ids)
            .await?;
        if valid_rooms.is_empty() {
            debug!(channel_id, "유효 구독자 없음 — 스킵");
            return Ok(vec![]);
        }

        // 채널 정보 추출
        let channel_info = stream.channel.clone();

        // 방별 AlarmNotification 생성
        let notifications: Vec<AlarmNotification> = valid_rooms
            .into_iter()
            .map(|room_id| {
                AlarmNotification::new(
                    room_id,
                    channel_info.clone(),
                    Some(stream.clone()),
                    minutes_until,
                    vec![],
                    schedule_change_msg.clone(),
                )
            })
            .collect();

        debug!(
            stream_id = stream.id,
            minutes_until,
            count = notifications.len(),
            "예정 스트림 알림 생성 완료"
        );

        Ok(notifications)
    }

    /// 라이브 catch-up 알림 생성
    ///
    /// 1. start_scheduled 확인 (없으면 start_actual 사용)
    /// 2. 어떤 분이라도 발송 이력이 있으면 스킵
    /// 3. 예정 알림이 LIVE_CATCHUP_SUPPRESS_WINDOW 내에 발송됐으면 스킵
    /// 4. minutes_until = 0으로 알림 생성
    pub async fn create_live_catchup_notification(
        &self,
        stream: &Stream,
        channel_id: &str,
        subscriber_room_ids: &[String],
    ) -> Result<Vec<AlarmNotification>, AlarmError> {
        // start_scheduled 보장 (없으면 start_actual 사용)
        let start_scheduled = stream
            .start_scheduled
            .or(stream.start_actual)
            .unwrap_or(DateTime::UNIX_EPOCH);

        // 이미 어떤 알림이라도 발송됐으면 스킵
        let already = self.dedup.is_already_notified(&stream.id).await?;
        if already {
            debug!(
                stream_id = stream.id,
                "라이브 catch-up: 이미 발송된 알림 — 스킵"
            );
            return Ok(vec![]);
        }

        // 유효 구독자 검증
        let valid_rooms = self
            .validate_subscribers(channel_id, subscriber_room_ids)
            .await?;
        if valid_rooms.is_empty() {
            debug!(channel_id, "라이브 catch-up: 유효 구독자 없음 — 스킵");
            return Ok(vec![]);
        }

        let channel_info = stream.channel.clone();
        let suppress_window = LIVE_CATCHUP_SUPPRESS_WINDOW;

        // 방별 catch-up 알림 생성 (예정 알림이 최근에 발송된 방은 제외)
        let mut notifications = Vec::with_capacity(valid_rooms.len());
        for room_id in valid_rooms {
            // 같은 이벤트의 예정 알림이 suppress window 내 발송됐으면 skip
            let suppressed = self
                .dedup
                .was_upcoming_event_notified_recently(&room_id, channel_id, stream, suppress_window)
                .await
                .unwrap_or(false);
            if suppressed {
                debug!(
                    room_id = room_id,
                    stream_id = stream.id,
                    "라이브 catch-up: suppress window 내 예정 알림 발송됨 — 스킵"
                );
                continue;
            }

            notifications.push(AlarmNotification::new(
                room_id,
                channel_info.clone(),
                Some(stream.clone()),
                0, // minutes_until = 0 (live)
                vec![],
                String::new(),
            ));
        }

        debug!(
            stream_id = stream.id,
            start_scheduled = ?start_scheduled,
            count = notifications.len(),
            "라이브 catch-up 알림 생성 완료"
        );

        Ok(notifications)
    }

    // ── 일정 변경 감지 ────────────────────────────────────────────────────────

    /// 저장된 NotifiedData와 현재 start_scheduled를 비교하여 일정 변경 메시지 반환
    /// 변경 없으면 빈 문자열 반환
    pub async fn detect_schedule_change(&self, stream: &Stream) -> Result<String, AlarmError> {
        let Some(current_start) = stream.start_scheduled else {
            return Ok(String::new());
        };

        // dedup을 통해 NotifiedData 조회 (HGETALL 우선, JSON 폴백)
        // 직접 valkey.get()을 쓰면 HASH 키에서 WRONGTYPE 오류 발생
        let key = alarm_core::keys::notified_key(&stream.id);
        let Some(data) = self.dedup.read_notified_data(&key).await else {
            // 이력 없음 → 변경 아님
            return Ok(String::new());
        };

        // 저장된 스케줄 파싱
        let Ok(saved_start) = data.start_scheduled.parse::<DateTime<Utc>>() else {
            return Ok(String::new());
        };

        // 1분 단위 비교 (초 제거 후 비교)
        let saved_min = saved_start.timestamp() / 60;
        let current_min = current_start.timestamp() / 60;
        if saved_min == current_min {
            return Ok(String::new());
        }

        // 일정 변경 감지 → claim 시도 (중복 발송 방지)
        let (_, claimed) = self
            .dedup
            .try_claim_schedule_transition(&stream.id, saved_start, current_start)
            .await?;
        if !claimed {
            // 다른 인스턴스가 이미 claim — 변경 알림 생략
            debug!(
                stream_id = stream.id,
                "일정 변경 claim 실패 — 다른 인스턴스가 처리 중"
            );
            return Ok(String::new());
        }

        let msg = format_schedule_change_message(saved_start, current_start);
        debug!(
            stream_id = stream.id,
            ?saved_start,
            ?current_start,
            msg,
            "일정 변경 감지"
        );

        Ok(msg)
    }

    // ── 구독자 검증 ───────────────────────────────────────────────────────────

    /// 각 room_id가 channel_id를 여전히 구독하고 있는지 확인
    /// "alarm:{room_id}" set의 members에 channel_id가 포함되는지 검사
    pub async fn validate_subscribers(
        &self,
        channel_id: &str,
        subscriber_room_ids: &[String],
    ) -> Result<Vec<String>, AlarmError> {
        if subscriber_room_ids.is_empty() {
            return Ok(vec![]);
        }

        let alarm_keys: Vec<String> = subscriber_room_ids
            .iter()
            .map(|room_id| format!("alarm:{room_id}"))
            .collect();

        match self.valkey.smembers_multi(&alarm_keys).await {
            Ok(room_members) => {
                if room_members.len() != subscriber_room_ids.len() {
                    warn!(
                        expected = subscriber_room_ids.len(),
                        got = room_members.len(),
                        "구독 확인 배치 응답 길이 불일치 — 개별 조회로 폴백"
                    );
                    return Ok(self
                        .validate_subscribers_single_lookup(channel_id, subscriber_room_ids)
                        .await);
                }

                let mut valid = Vec::with_capacity(subscriber_room_ids.len());
                for (room_id, members) in subscriber_room_ids.iter().zip(room_members.iter()) {
                    if members.iter().any(|member| member == channel_id) {
                        valid.push(room_id.clone());
                    }
                }
                Ok(valid)
            }
            Err(err) => {
                warn!(error = %err, "구독 확인 배치 조회 실패 — 개별 조회로 폴백");
                Ok(self
                    .validate_subscribers_single_lookup(channel_id, subscriber_room_ids)
                    .await)
            }
        }
    }

    async fn validate_subscribers_single_lookup(
        &self,
        channel_id: &str,
        subscriber_room_ids: &[String],
    ) -> Vec<String> {
        const SUBSCRIBER_VALIDATION_CONCURRENCY: usize = 32;

        let mut valid = Vec::with_capacity(subscriber_room_ids.len());
        let room_ids: Vec<String> = subscriber_room_ids.to_vec();
        let mut checks = stream::iter(room_ids.into_iter().map(|room_id| async move {
            let alarm_key = format!("alarm:{room_id}");
            match self.valkey.smembers(&alarm_key).await {
                Ok(members) => Ok((room_id, members)),
                Err(err) => Err((room_id, err)),
            }
        }))
        .buffer_unordered(SUBSCRIBER_VALIDATION_CONCURRENCY);

        while let Some(result) = checks.next().await {
            match result {
                Ok((room_id, members)) => {
                    if members.iter().any(|member| member == channel_id) {
                        valid.push(room_id);
                    }
                }
                Err((room_id, err)) => {
                    warn!(room_id = room_id.as_str(), error = %err, "구독 확인 실패 — 스킵");
                }
            }
        }

        valid
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests;
