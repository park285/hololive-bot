use std::{collections::HashMap, sync::Arc, time::Duration as StdDuration};

use alarm_core::{
    constants::{LOCAL_FALLBACK_CLEANUP_MAX_KEYS, LOCAL_FALLBACK_DEDUP_TTL, NOTIFICATION_SENT_TTL},
    error::AlarmError,
    keys::{
        build_logical_event_claim_key, build_notify_claim_key, build_schedule_transition_key,
        build_upcoming_event_key, notification_category, notified_key,
    },
    model::{NotifiedData, Stream, UpcomingEventNotifiedData},
};
use alarm_infra::valkey::ValkeyClient;
use chrono::{DateTime, Utc};
use moka::sync::Cache;

// ─────────────────────────────────────────────────────────────────────────────
// DedupService: SETNX 기반 4단계 중복 방지 서비스
// ─────────────────────────────────────────────────────────────────────────────

/// SETNX 기반 알림 중복 방지 서비스
/// Valkey 장애 시 로컬 HashMap 폴백으로 dedup을 유지한다.
pub struct DedupService {
    /// Valkey 클라이언트 (DI)
    valkey: Arc<dyn ValkeyClient>,
    /// 알림 대상 분 목록 (예: [5, 3, 1])
    target_minutes: Vec<i32>,
    /// Valkey 장애 시 로컬 폴백 dedup TTL 캐시 (존재 여부만 추적, TTL은 moka가 관리)
    local_keys: Cache<String, ()>,
}

impl DedupService {
    /// DedupService 생성
    pub fn new(valkey: Arc<dyn ValkeyClient>, target_minutes: Vec<i32>) -> Self {
        Self {
            valkey,
            target_minutes,
            local_keys: Cache::builder()
                .max_capacity(LOCAL_FALLBACK_CLEANUP_MAX_KEYS as u64)
                .time_to_live(LOCAL_FALLBACK_DEDUP_TTL)
                .build(),
        }
    }

    // ── 공개 API ─────────────────────────────────────────────────────────────

    /// 알림 발송 권한 선점 시도
    /// start_scheduled가 epoch(zero)이면 ("", false) 반환
    pub async fn try_claim_notification(
        &self,
        room_id: &str,
        stream_id: &str,
        start_scheduled: DateTime<Utc>,
        minutes_until: i32,
    ) -> Result<(String, bool), AlarmError> {
        // epoch(zero) 가드
        if start_scheduled == DateTime::UNIX_EPOCH {
            return Ok((String::new(), false));
        }

        let category = notification_category(&self.target_minutes, minutes_until);
        let key = build_notify_claim_key(room_id, stream_id, start_scheduled, &category);
        let acquired = self.try_claim_key(&key, NOTIFICATION_SENT_TTL).await;
        Ok((key, acquired))
    }

    /// 논리적 이벤트 claim 시도 (stream_id 변경 대응)
    /// stream.start_scheduled가 없으면 ("", false) 반환
    pub async fn try_claim_logical_event(
        &self,
        room_id: &str,
        channel_id: &str,
        stream: &Stream,
        minutes_until: i32,
    ) -> Result<(String, bool), AlarmError> {
        // start_scheduled 없으면 스킵
        let Some(start) = stream.start_scheduled else {
            return Ok((String::new(), false));
        };
        if start == DateTime::UNIX_EPOCH {
            return Ok((String::new(), false));
        }

        let category = notification_category(&self.target_minutes, minutes_until);
        let key = build_logical_event_claim_key(
            room_id,
            channel_id,
            &stream.id,
            &stream.title,
            start,
            &category,
        );
        let acquired = self.try_claim_key(&key, NOTIFICATION_SENT_TTL).await;
        Ok((key, acquired))
    }

    /// 일정 변경 전환 claim 시도
    pub async fn try_claim_schedule_transition(
        &self,
        stream_id: &str,
        old_scheduled: DateTime<Utc>,
        new_scheduled: DateTime<Utc>,
    ) -> Result<(String, bool), AlarmError> {
        let key = build_schedule_transition_key(stream_id, old_scheduled, new_scheduled);
        let acquired = self.try_claim_key(&key, NOTIFICATION_SENT_TTL).await;
        Ok((key, acquired))
    }

    /// claim 키 해제 (발송 실패 시 재시도 허용)
    pub async fn release_claims(&self, keys: &[String]) -> Result<(), AlarmError> {
        if keys.is_empty() {
            return Ok(());
        }
        // 로컬 폴백 해제
        self.release_local_dedup_claims(keys);

        // Valkey DEL
        let key_refs: Vec<&str> = keys.iter().map(|s| s.as_str()).collect();
        self.valkey.del(&key_refs).await?;
        Ok(())
    }

    /// 알림 발송 이력 기록 (HSET 원자적 갱신)
    /// 스케줄 변경 시 기존 해시 삭제 후 재생성한다.
    pub async fn mark_as_notified(
        &self,
        stream_id: &str,
        start_scheduled: DateTime<Utc>,
        minutes_until: i32,
    ) -> Result<(), AlarmError> {
        let key = notified_key(stream_id);
        let scheduled_str = format_scheduled(start_scheduled);

        // 기존 스케줄 확인 (변경 감지)
        let existing_scheduled = self.valkey.hget(&key, "start_scheduled").await?;
        if existing_scheduled
            .as_deref()
            .is_some_and(|e| e != scheduled_str)
        {
            // 스케줄 변경 → 기존 해시 삭제 후 재생성
            self.valkey.del(&[&key]).await?;
        }

        // 원자적 필드 갱신
        self.valkey
            .hset(&key, "start_scheduled", &scheduled_str)
            .await?;
        self.valkey
            .hset(&key, &minutes_until.to_string(), "1")
            .await?;
        self.valkey.expire(&key, NOTIFICATION_SENT_TTL).await?;
        Ok(())
    }

    /// 현재 스케줄에서 해당 분(minutesUntil)에 이미 알림이 발송됐는지 확인
    ///
    /// 1회 발송 정책:
    ///   - 캐시 미스 → false (발송 허용)
    ///   - 스케줄 변경됨 → false (재발송 허용)
    ///   - minutesUntil == 0 (live) → SentAt[0] 존재 여부
    ///   - target 분 → 같은 스케줄에서 어떤 target 분이라도 발송됐으면 → true (1회 정책)
    ///   - non-target 분 → SentAt[minutesUntil] 존재 여부
    pub async fn is_already_notified_for_schedule(
        &self,
        stream_id: &str,
        start_scheduled: DateTime<Utc>,
        minutes_until: i32,
    ) -> Result<bool, AlarmError> {
        let key = notified_key(stream_id);
        let scheduled_str = format_scheduled(start_scheduled);

        let Some(data) = self.read_notified_data(&key).await else {
            // 캐시 미스 → 발송 허용
            return Ok(false);
        };

        // 스케줄 변경됨 → 발송 허용
        if data.start_scheduled != scheduled_str {
            return Ok(false);
        }

        // minutesUntil 판정
        if minutes_until == 0 {
            // live catchup: SentAt[0] 확인
            return Ok(data.sent_at.contains_key(&0));
        }

        // target 분: 어떤 target이라도 발송됐으면 차단 (1회 정책)
        if self.is_target_minute(minutes_until) {
            for target in &self.target_minutes {
                if data.sent_at.contains_key(target) {
                    return Ok(true);
                }
            }
            return Ok(false);
        }

        // non-target: 해당 분만 확인
        Ok(data.sent_at.contains_key(&minutes_until))
    }

    /// live catchup용: 어떤 분이라도 발송 이력이 있으면 true
    pub async fn is_already_notified(&self, stream_id: &str) -> Result<bool, AlarmError> {
        let key = notified_key(stream_id);
        let data = self.read_notified_data(&key).await;
        Ok(data.map(|d| !d.sent_at.is_empty()).unwrap_or(false))
    }

    /// 예정 알림 발송 시각을 이벤트 단위로 기록
    pub async fn mark_upcoming_event_notified(
        &self,
        room_id: &str,
        channel_id: &str,
        stream: &Stream,
    ) -> Result<(), AlarmError> {
        let Some(start) = stream.start_scheduled else {
            return Ok(());
        };
        if start == DateTime::UNIX_EPOCH {
            return Ok(());
        }

        let key = build_upcoming_event_key(room_id, channel_id, &stream.id, &stream.title, start);
        let data = UpcomingEventNotifiedData {
            notified_at: Utc::now().to_rfc3339(),
        };
        let json = serde_json::to_string(&data)?;
        self.valkey.set(&key, &json, NOTIFICATION_SENT_TTL).await?;
        Ok(())
    }

    /// 동일 이벤트의 예정 알림이 window 내에 발송됐는지 확인
    pub async fn was_upcoming_event_notified_recently(
        &self,
        room_id: &str,
        channel_id: &str,
        stream: &Stream,
        window: StdDuration,
    ) -> Result<bool, AlarmError> {
        let Some(start) = stream.start_scheduled else {
            return Ok(false);
        };
        if start == DateTime::UNIX_EPOCH {
            return Ok(false);
        }

        let key = build_upcoming_event_key(room_id, channel_id, &stream.id, &stream.title, start);
        let raw = match self.valkey.get(&key).await? {
            Some(v) => v,
            None => return Ok(false),
        };

        let data: UpcomingEventNotifiedData = serde_json::from_str(&raw)?;
        if data.notified_at.is_empty() {
            return Ok(false);
        }

        let notified_at = match DateTime::parse_from_rfc3339(&data.notified_at) {
            Ok(t) => t.with_timezone(&Utc),
            Err(_) => return Ok(false),
        };

        if window.is_zero() {
            return Ok(false);
        }

        let elapsed = Utc::now() - notified_at;
        let window_dur = chrono::Duration::from_std(window).unwrap_or(chrono::Duration::zero());
        Ok(elapsed <= window_dur)
    }

    // ── 비공개 헬퍼 ──────────────────────────────────────────────────────────

    /// SETNX 기반 키 선점 (Valkey 장애 시 로컬 폴백)
    async fn try_claim_key(&self, key: &str, ttl: StdDuration) -> bool {
        match self.valkey.set_nx(key, "1", ttl).await {
            Ok(acquired) => acquired,
            Err(e) => {
                // Valkey 장애 → 로컬 폴백
                let fallback_ttl = if ttl.is_zero() || ttl > LOCAL_FALLBACK_DEDUP_TTL {
                    LOCAL_FALLBACK_DEDUP_TTL
                } else {
                    ttl
                };
                let local_ok = self.try_local_dedup_claim(key, fallback_ttl);
                tracing::warn!(
                    key,
                    fallback_acquired = local_ok,
                    error = %e,
                    "SETNX claim 실패, 로컬 폴백 사용"
                );
                local_ok
            }
        }
    }

    /// 로컬 dedup 클레임 시도
    /// moka의 time_to_live가 TTL을 관리하므로 Instant 비교 불필요
    fn try_local_dedup_claim(&self, key: &str, _ttl: StdDuration) -> bool {
        if self.local_keys.contains_key(key) {
            return false;
        }

        self.local_keys.insert(key.to_string(), ());
        true
    }

    /// 로컬 dedup claim 해제
    fn release_local_dedup_claims(&self, keys: &[String]) {
        for key in keys {
            self.local_keys.invalidate(key);
        }
    }

    /// minutes_until이 target_minutes에 포함되는지 확인
    fn is_target_minute(&self, minutes_until: i32) -> bool {
        self.target_minutes.contains(&minutes_until)
    }

    /// Valkey에서 NotifiedData 조회 (HGETALL 우선, 기존 JSON 폴백)
    pub(crate) async fn read_notified_data(&self, key: &str) -> Option<NotifiedData> {
        // HGETALL로 해시 데이터 조회 시도
        if let Ok(fields) = self.valkey.hget_all(key).await
            && !fields.is_empty()
        {
            let start_scheduled = fields.get("start_scheduled").cloned().unwrap_or_default();
            let sent_at: HashMap<i32, bool> = fields
                .iter()
                .filter(|(k, _)| k.as_str() != "start_scheduled")
                .filter_map(|(k, _)| k.parse::<i32>().ok().map(|m| (m, true)))
                .collect();
            return Some(NotifiedData {
                start_scheduled,
                sent_at,
            });
        }

        // 폴백: 기존 JSON 형식 (GET → parse)
        let raw = self.valkey.get(key).await.ok()??;
        serde_json::from_str::<NotifiedData>(&raw).ok()
    }
}

/// DateTime<Utc>를 RFC3339 분 단위로 포맷 (초 버림)
fn format_scheduled(dt: DateTime<Utc>) -> String {
    use chrono::DurationRound;
    let truncated = dt
        .duration_trunc(chrono::Duration::minutes(1))
        .unwrap_or(dt);
    truncated.to_rfc3339()
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests;
