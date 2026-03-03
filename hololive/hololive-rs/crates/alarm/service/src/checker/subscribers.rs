use alarm_core::error::AlarmError;
use futures::{StreamExt, stream};
use tracing::warn;

use super::YouTubeChecker;

impl YouTubeChecker {
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
