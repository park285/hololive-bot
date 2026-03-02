use std::sync::Arc;

use alarm_core::{
    error::AlarmError,
    model::{AlarmNotification, AlarmQueueEnvelope},
};
use alarm_infra::valkey::ValkeyClient;
use chrono::Utc;
use tracing::debug;

/// 알림 큐 발행 키
const ALARM_DISPATCH_QUEUE: &str = "alarm:dispatch:queue";

/// 알림 발송 봉투를 Valkey List로 발행하는 퍼블리셔
pub struct QueuePublisher {
    valkey: Arc<dyn ValkeyClient>,
}

impl QueuePublisher {
    pub fn new(valkey: Arc<dyn ValkeyClient>) -> Self {
        Self { valkey }
    }

    /// 알림 봉투를 JSON 직렬화 후 Valkey 큐에 LPUSH 한다.
    pub async fn publish(
        &self,
        notification: &AlarmNotification,
        claim_keys: Vec<String>,
    ) -> Result<(), AlarmError> {
        let envelope = AlarmQueueEnvelope {
            notification: notification.clone(),
            claim_keys,
            enqueued_at: Utc::now().to_rfc3339(),
            version: 1,
        };

        // Serialization variant에 #[from] 적용됨 → ? 직접 사용 가능
        let json = serde_json::to_string(&envelope)?;

        self.valkey.lpush(ALARM_DISPATCH_QUEUE, &json).await?;

        debug!(
            room_id = notification.room_id,
            queue = ALARM_DISPATCH_QUEUE,
            "알림 큐 발행 완료"
        );

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use alarm_core::model::AlarmNotification;
    use alarm_infra::valkey::MockValkeyClient;

    use super::*;

    fn make_notification(room_id: &str) -> AlarmNotification {
        AlarmNotification::new(room_id.into(), None, None, 5, vec![], String::new())
    }

    #[tokio::test]
    async fn publish_enqueues_valid_json() {
        let valkey = Arc::new(MockValkeyClient::new());
        let publisher = QueuePublisher::new(valkey.clone() as Arc<dyn ValkeyClient>);

        let notification = make_notification("room1");
        let claim_keys = vec!["claim:key1".to_owned()];

        publisher.publish(&notification, claim_keys).await.unwrap();

        let list = valkey.lstore.get(ALARM_DISPATCH_QUEUE).unwrap();
        assert_eq!(list.len(), 1);

        // 역직렬화 가능 확인
        let envelope: AlarmQueueEnvelope = serde_json::from_str(&list[0]).unwrap();
        assert_eq!(envelope.notification.room_id, "room1");
        assert_eq!(envelope.version, 1);
        assert_eq!(envelope.claim_keys, vec!["claim:key1"]);
    }

    #[tokio::test]
    async fn publish_multiple_preserves_order() {
        let valkey = Arc::new(MockValkeyClient::new());
        let publisher = QueuePublisher::new(valkey.clone() as Arc<dyn ValkeyClient>);

        publisher
            .publish(&make_notification("room1"), vec![])
            .await
            .unwrap();
        publisher
            .publish(&make_notification("room2"), vec![])
            .await
            .unwrap();

        let list = valkey.lstore.get(ALARM_DISPATCH_QUEUE).unwrap();
        assert_eq!(list.len(), 2);

        // LPUSH이므로 최신이 앞에
        let first: AlarmQueueEnvelope = serde_json::from_str(&list[0]).unwrap();
        let second: AlarmQueueEnvelope = serde_json::from_str(&list[1]).unwrap();
        assert_eq!(first.notification.room_id, "room2");
        assert_eq!(second.notification.room_id, "room1");
    }
}
