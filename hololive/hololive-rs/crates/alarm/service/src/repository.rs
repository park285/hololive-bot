use std::{
    collections::{BTreeSet, HashMap},
    sync::Arc,
};

use async_trait::async_trait;
use chrono::Utc;
use shared_core::{
    error::SharedError,
    keys::{ALARM_REGISTRY_KEY, alarm_key},
    model::alarm::{Alarm, AlarmType},
};
use shared_infra::valkey::ValkeyClient;

#[async_trait]
pub trait AlarmCRUD: Send + Sync {
    async fn add_alarm(
        &self,
        room_id: &str,
        channel_id: &str,
        alarm_types: &[AlarmType],
    ) -> Result<(), SharedError>;
    async fn remove_alarm(&self, room_id: &str, channel_id: &str) -> Result<(), SharedError>;
    async fn get_room_alarms(&self, room_id: &str) -> Result<Vec<Alarm>, SharedError>;
    async fn clear_room_alarms(&self, room_id: &str) -> Result<(), SharedError>;
    async fn get_all_alarm_keys(&self) -> Result<Vec<String>, SharedError>;
}

pub struct AlarmRepository {
    client: Arc<dyn ValkeyClient>,
}

impl AlarmRepository {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self { client }
    }

    async fn load_room_alarms(&self, room_id: &str) -> Result<HashMap<String, Alarm>, SharedError> {
        let key = alarm_key(room_id);
        let Some(payload) = self.client.get(&key).await? else {
            return Ok(HashMap::new());
        };

        serde_json::from_str::<HashMap<String, Alarm>>(&payload)
            .or_else(|_| serde_json::from_str::<Vec<Alarm>>(&payload).map(index_by_channel))
            .map_err(SharedError::from)
    }

    async fn save_room_alarms(
        &self,
        room_id: &str,
        alarms: &HashMap<String, Alarm>,
    ) -> Result<(), SharedError> {
        let key = alarm_key(room_id);
        let payload = serde_json::to_string(alarms)?;
        self.client.set(&key, &payload, None).await
    }

    async fn load_registry(&self) -> Result<BTreeSet<String>, SharedError> {
        let Some(payload) = self.client.get(ALARM_REGISTRY_KEY).await? else {
            return Ok(BTreeSet::new());
        };

        let rooms: Vec<String> = serde_json::from_str(&payload)?;
        Ok(rooms.into_iter().collect())
    }

    async fn save_registry(&self, registry: &BTreeSet<String>) -> Result<(), SharedError> {
        let rooms: Vec<&str> = registry.iter().map(String::as_str).collect();
        let payload = serde_json::to_string(&rooms)?;
        self.client.set(ALARM_REGISTRY_KEY, &payload, None).await
    }

    fn make_alarm(
        room_id: &str,
        channel_id: &str,
        alarm_types: &[AlarmType],
        existing: Option<&Alarm>,
    ) -> Alarm {
        let normalized_types: Vec<AlarmType> = if alarm_types.is_empty() {
            AlarmType::all().to_vec()
        } else {
            alarm_types.to_vec()
        };

        let created_at = existing.map_or_else(Utc::now, |alarm| alarm.created_at);

        Alarm {
            id: existing.and_then(|alarm| alarm.id),
            room_id: room_id.to_owned(),
            user_id: existing
                .map(|alarm| alarm.user_id.clone())
                .unwrap_or_default(),
            channel_id: channel_id.to_owned(),
            member_name: existing.and_then(|alarm| alarm.member_name.clone()),
            room_name: existing.and_then(|alarm| alarm.room_name.clone()),
            user_name: existing.and_then(|alarm| alarm.user_name.clone()),
            alarm_types: normalized_types,
            created_at,
        }
    }
}

#[async_trait]
impl AlarmCRUD for AlarmRepository {
    async fn add_alarm(
        &self,
        room_id: &str,
        channel_id: &str,
        alarm_types: &[AlarmType],
    ) -> Result<(), SharedError> {
        let mut alarms = self.load_room_alarms(room_id).await?;
        let existing = alarms.get(channel_id);
        let alarm = Self::make_alarm(room_id, channel_id, alarm_types, existing);
        alarms.insert(channel_id.to_owned(), alarm);
        self.save_room_alarms(room_id, &alarms).await?;

        let mut registry = self.load_registry().await?;
        registry.insert(room_id.to_owned());
        self.save_registry(&registry).await
    }

    async fn remove_alarm(&self, room_id: &str, channel_id: &str) -> Result<(), SharedError> {
        let mut alarms = self.load_room_alarms(room_id).await?;
        alarms.remove(channel_id);

        if alarms.is_empty() {
            let key = alarm_key(room_id);
            let delete_keys = [key.as_str()];
            self.client.del(&delete_keys).await?;

            let mut registry = self.load_registry().await?;
            registry.remove(room_id);
            self.save_registry(&registry).await?;
            return Ok(());
        }

        self.save_room_alarms(room_id, &alarms).await
    }

    async fn get_room_alarms(&self, room_id: &str) -> Result<Vec<Alarm>, SharedError> {
        let alarms = self.load_room_alarms(room_id).await?;
        let mut values: Vec<Alarm> = alarms.into_values().collect();
        values.sort_by(|left, right| left.channel_id.cmp(&right.channel_id));
        Ok(values)
    }

    async fn clear_room_alarms(&self, room_id: &str) -> Result<(), SharedError> {
        let key = alarm_key(room_id);
        let delete_keys = [key.as_str()];
        self.client.del(&delete_keys).await?;

        let mut registry = self.load_registry().await?;
        registry.remove(room_id);
        self.save_registry(&registry).await
    }

    async fn get_all_alarm_keys(&self) -> Result<Vec<String>, SharedError> {
        let registry = self.load_registry().await?;
        Ok(registry
            .into_iter()
            .map(|room_id| alarm_key(&room_id))
            .collect())
    }
}

fn index_by_channel(alarms: Vec<Alarm>) -> HashMap<String, Alarm> {
    alarms
        .into_iter()
        .map(|alarm| (alarm.channel_id.clone(), alarm))
        .collect()
}
