use std::sync::Arc;

use alarm_core::{error::AlarmError, model::AlarmNotification};
use futures::{StreamExt, stream};
use tracing::{debug, warn};

use super::{dedup::DedupService, queue::QueuePublisher, tier::TieredScheduler};

// ─────────────────────────────────────────────────────────────────────────────
// NotifierResult: 발송 통계
// ─────────────────────────────────────────────────────────────────────────────

/// 알림 발송 결과 통계
pub struct NotifierResult {
    pub sent: usize,
    pub skipped: usize,
    pub failed: usize,
}

// ─────────────────────────────────────────────────────────────────────────────
// SendResult: 개별 알림 발송 결과
// ─────────────────────────────────────────────────────────────────────────────

/// 개별 알림 처리 결과
enum SendResult {
    /// 정상 발송됨
    Sent,
    /// claim 실패로 스킵됨 (중복 방지)
    Skipped,
    /// 발송 실패
    Failed,
}

// ─────────────────────────────────────────────────────────────────────────────
// Notifier: Valkey 큐를 통해 Go에 알림 위임
// ─────────────────────────────────────────────────────────────────────────────

/// 알림 발송기 — dedup claim 후 Valkey 큐에 봉투를 발행한다.
/// 렌더링 및 Iris 발송은 Go 측에서 BRPOP 후 처리한다.
pub struct Notifier {
    queue: Arc<QueuePublisher>,
    dedup: Arc<DedupService>,
    scheduler: Arc<TieredScheduler>,
}

impl Notifier {
    pub fn new(
        queue: Arc<QueuePublisher>,
        dedup: Arc<DedupService>,
        scheduler: Arc<TieredScheduler>,
    ) -> Self {
        Self {
            queue,
            dedup,
            scheduler,
        }
    }

    // ── 공개 API ─────────────────────────────────────────────────────────────

    /// 알림 목록 전체 발송
    /// 각 알림을 독립적으로 처리 — 개별 실패가 나머지 처리를 중단시키지 않는다.
    pub async fn send_notifications(
        &self,
        notifications: Vec<AlarmNotification>,
    ) -> Result<NotifierResult, AlarmError> {
        const NOTIFIER_SEND_CONCURRENCY: usize = 32;

        let mut result = NotifierResult {
            sent: 0,
            skipped: 0,
            failed: 0,
        };

        let mut sending = stream::iter(
            notifications
                .into_iter()
                .map(|notification| async move { self.send_single(&notification).await }),
        )
        .buffer_unordered(NOTIFIER_SEND_CONCURRENCY);

        while let Some(send_result) = sending.next().await {
            match send_result {
                SendResult::Sent => result.sent += 1,
                SendResult::Skipped => result.skipped += 1,
                SendResult::Failed => result.failed += 1,
            }
        }

        debug!(
            sent = result.sent,
            skipped = result.skipped,
            failed = result.failed,
            "알림 발송 완료"
        );

        Ok(result)
    }

    // ── 내부 처리 ─────────────────────────────────────────────────────────────

    /// 개별 알림 발송 흐름
    ///
    /// 1. stream/start_scheduled 조회
    /// 2. notify claim 시도 → 실패 시 Skipped
    /// 3. logical event claim 시도 → 실패 시 notify claim 해제 후 Skipped
    /// 4. claim 키 수집
    /// 5. 큐 발행 (실패 시 claim 전체 해제)
    /// 6. Rust in-memory 스케줄러 업데이트 (mark_as_notified/mark_upcoming은 Go 담당)
    async fn send_single(&self, notification: &AlarmNotification) -> SendResult {
        let stream = match &notification.stream {
            Some(s) => s,
            None => {
                warn!(room_id = notification.room_id, "stream 정보 없음 — 스킵");
                return SendResult::Skipped;
            }
        };

        let start_scheduled = match stream.start_scheduled {
            Some(t) => t,
            None => {
                warn!(
                    room_id = notification.room_id,
                    stream_id = stream.id,
                    "start_scheduled 없음 — 스킵"
                );
                return SendResult::Skipped;
            }
        };

        // 1. notify claim 시도
        let (notify_key, notify_ok) = match self
            .dedup
            .try_claim_notification(
                &notification.room_id,
                &stream.id,
                start_scheduled,
                notification.minutes_until,
            )
            .await
        {
            Ok(pair) => pair,
            Err(e) => {
                warn!(
                    room_id = notification.room_id,
                    error = %e,
                    "notify claim 오류 — 스킵"
                );
                return SendResult::Skipped;
            }
        };

        if !notify_ok {
            debug!(
                room_id = notification.room_id,
                stream_id = stream.id,
                "notify claim 실패 (중복) — 스킵"
            );
            return SendResult::Skipped;
        }

        // 2. logical event claim 시도
        let (logical_key, logical_ok) = match self
            .dedup
            .try_claim_logical_event(
                &notification.room_id,
                &stream.channel_id,
                stream,
                notification.minutes_until,
            )
            .await
        {
            Ok(pair) => pair,
            Err(e) => {
                warn!(
                    room_id = notification.room_id,
                    error = %e,
                    "logical event claim 오류 — notify claim 해제 후 스킵"
                );
                let _ = self.dedup.release_claims(&[notify_key]).await;
                return SendResult::Skipped;
            }
        };

        if !logical_ok {
            debug!(
                room_id = notification.room_id,
                stream_id = stream.id,
                "logical event claim 실패 (중복) — notify claim 해제 후 스킵"
            );
            let _ = self.dedup.release_claims(&[notify_key]).await;
            return SendResult::Skipped;
        }

        // 3. claim 키 수집
        let mut claim_keys = vec![notify_key];
        if !logical_key.is_empty() {
            claim_keys.push(logical_key);
        }

        // 4. 큐 발행
        if let Err(e) = self.queue.publish(notification, claim_keys.clone()).await {
            warn!(
                room_id = notification.room_id,
                stream_id = stream.id,
                error = %e,
                "알림 큐 발행 실패 — claim 해제"
            );
            let _ = self.dedup.release_claims(&claim_keys).await;
            return SendResult::Failed;
        }

        // 5. Rust in-memory 스케줄러 업데이트 (mark_as_notified/mark_upcoming은 Go 담당)
        let channel_id = &stream.channel_id;
        self.scheduler.mark_channel_recently_notified(channel_id);

        debug!(
            room_id = notification.room_id,
            stream_id = stream.id,
            minutes_until = notification.minutes_until,
            "알림 큐 발행 성공"
        );

        SendResult::Sent
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 테스트
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use alarm_core::{
        constants::DEFAULT_TARGET_MINUTES,
        model::{Channel, Stream, StreamStatus},
    };
    use alarm_infra::valkey::{MockValkeyClient, ValkeyClient};
    use chrono::Utc;

    use super::*;

    // ── 테스트 헬퍼 ──────────────────────────────────────────────────────────

    fn make_notifier_parts() -> (
        Arc<MockValkeyClient>,
        Arc<DedupService>,
        Arc<TieredScheduler>,
    ) {
        let valkey = Arc::new(MockValkeyClient::new());
        let dedup = Arc::new(DedupService::new(
            valkey.clone() as Arc<dyn ValkeyClient>,
            DEFAULT_TARGET_MINUTES.to_vec(),
        ));
        let scheduler = Arc::new(TieredScheduler::new());
        (valkey, dedup, scheduler)
    }

    fn make_notifier(
        valkey: Arc<MockValkeyClient>,
        dedup: Arc<DedupService>,
        scheduler: Arc<TieredScheduler>,
    ) -> Notifier {
        let queue = Arc::new(QueuePublisher::new(valkey as Arc<dyn ValkeyClient>));
        Notifier::new(queue, dedup, scheduler)
    }

    fn make_stream(id: &str, channel_id: &str) -> Stream {
        let start = Utc::now() + chrono::Duration::minutes(5);
        Stream {
            id: id.into(),
            title: "테스트 방송".into(),
            channel_id: channel_id.into(),
            channel_name: "채널명".into(),
            status: StreamStatus::Upcoming,
            start_scheduled: Some(start),
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: Some(Channel {
                id: channel_id.into(),
                name: "채널명".into(),
                english_name: None,
                photo: None,
                twitter: None,
                video_count: None,
                subscriber_count: None,
                org: Some("Hololive".into()),
                suborg: None,
                group: None,
            }),
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

    fn make_notification(room_id: &str, stream: Stream, minutes_until: i32) -> AlarmNotification {
        AlarmNotification::new(
            room_id.into(),
            stream.channel.clone(),
            Some(stream),
            minutes_until,
            vec![],
            String::new(),
        )
    }

    // ── 테스트 케이스 ─────────────────────────────────────────────────────────

    /// 1. claim 성공 → 큐 발행 → lstore에 envelope 존재
    #[tokio::test]
    async fn send_notifications_claims_succeed_queue_published() {
        let (valkey, dedup, scheduler) = make_notifier_parts();
        let notifier = make_notifier(
            Arc::clone(&valkey),
            Arc::clone(&dedup),
            Arc::clone(&scheduler),
        );

        let stream = make_stream("vid001", "UC_test");
        let notification = make_notification("room1", stream, 5);

        let result = notifier
            .send_notifications(vec![notification])
            .await
            .unwrap();

        assert_eq!(result.sent, 1);
        assert_eq!(result.skipped, 0);
        assert_eq!(result.failed, 0);

        // 큐에 envelope가 존재해야 함
        let list = valkey.list_items("alarm:dispatch:queue");
        assert_eq!(list.len(), 1);

        let envelope: alarm_core::model::AlarmQueueEnvelope =
            serde_json::from_str(&list[0]).unwrap();
        assert_eq!(envelope.notification.room_id, "room1");
        assert_eq!(envelope.version, 1);
    }

    /// 2. 동일 알림 두 번 발송 → 두 번째는 skipped (claim 중복)
    #[tokio::test]
    async fn send_notifications_duplicate_claim_skipped() {
        let (valkey, dedup, scheduler) = make_notifier_parts();
        let notifier = make_notifier(
            Arc::clone(&valkey),
            Arc::clone(&dedup),
            Arc::clone(&scheduler),
        );

        let stream = make_stream("vid002", "UC_dup");
        let n1 = make_notification("room1", stream.clone(), 5);
        let n2 = make_notification("room1", stream, 5);

        let r1 = notifier.send_notifications(vec![n1]).await.unwrap();
        assert_eq!(r1.sent, 1);

        let r2 = notifier.send_notifications(vec![n2]).await.unwrap();
        assert_eq!(r2.skipped, 1);
        assert_eq!(r2.sent, 0);
    }

    /// 3. 빈 목록 → NotifierResult { 0, 0, 0 }
    #[tokio::test]
    async fn send_notifications_empty_list_returns_zero() {
        let (valkey, dedup, scheduler) = make_notifier_parts();
        let notifier = make_notifier(valkey, dedup, scheduler);

        let result = notifier.send_notifications(vec![]).await.unwrap();

        assert_eq!(result.sent, 0);
        assert_eq!(result.skipped, 0);
        assert_eq!(result.failed, 0);
    }

    /// 4. stream 없음 → skipped
    #[tokio::test]
    async fn send_notifications_no_stream_skipped() {
        let (valkey, dedup, scheduler) = make_notifier_parts();
        let notifier = make_notifier(valkey, dedup, scheduler);

        let notification =
            AlarmNotification::new("room1".into(), None, None, 5, vec![], String::new());

        let result = notifier
            .send_notifications(vec![notification])
            .await
            .unwrap();
        assert_eq!(result.skipped, 1);
    }
}
