use alarm_core::{
    constants::LIVE_CATCHUP_SUPPRESS_WINDOW,
    error::AlarmError,
    model::{AlarmNotification, Stream},
};
use chrono::{DateTime, Utc};
use tracing::debug;

use super::YouTubeChecker;
use crate::checker_helpers::{
    format_schedule_change_message, is_target_minute, minutes_until_floor,
};

impl YouTubeChecker {
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
}
