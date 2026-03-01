use std::collections::HashMap;

use alarm_core::{error::AlarmError, keys::ALARM_CHANNEL_REGISTRY_KEY};

use super::{
    AlarmScheduler, CHANNEL_SUBSCRIBERS_KEY_PREFIX, CHZZK_CHANNEL_MAP_KEY, TWITCH_LOGIN_MAP_KEY,
};

impl AlarmScheduler {
    /// Chzzk 채널 매핑 + 구독자 맵 조회
    ///
    /// 반환:
    ///   - channel_mappings: youtube_channel_id → chzzk_channel_id
    ///   - subscriber_map: youtube_channel_id → [room_id, ...]
    pub(super) async fn fetch_chzzk_mappings(
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
    pub(super) async fn fetch_twitch_mappings(
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
    pub(super) async fn fetch_subscriber_map<'a, I>(
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
