use std::{
    collections::HashMap,
    sync::{Arc, RwLock},
    time::Duration,
};

use async_trait::async_trait;
use shared_core::{error::SharedError, model::member::Member};
use shared_infra::valkey::ValkeyClient;

use crate::{MemberDataProvider, normalize_key};

const MEMBER_CHANNEL_KEY_PREFIX: &str = "member:channel:";
const MEMBER_NAME_KEY_PREFIX: &str = "member:name:";
const MEMBER_ALIAS_KEY_PREFIX: &str = "member:alias:";
const MEMBER_CHANNEL_IDS_KEY: &str = "member:channel_ids";

#[derive(Default)]
struct MemberMemoryCache {
    by_channel_id: HashMap<String, Member>,
    by_name: HashMap<String, Member>,
    alias_to_channel: HashMap<String, String>,
    channel_ids: Vec<String>,
}

pub struct MemberCache {
    client: Arc<dyn ValkeyClient>,
    memory: RwLock<MemberMemoryCache>,
}

impl MemberCache {
    pub fn new(client: Arc<dyn ValkeyClient>) -> Self {
        Self {
            client,
            memory: RwLock::new(MemberMemoryCache::default()),
        }
    }

    pub async fn upsert_members(
        &self,
        members: &[Member],
        ttl: Option<Duration>,
    ) -> Result<(), SharedError> {
        let mut channel_ids = Vec::new();

        for member in members {
            if !member.channel_id.is_empty() {
                channel_ids.push(member.channel_id.clone());
            }

            self.remember_member(member.clone());
            self.cache_member(member, ttl).await?;
        }

        let payload = serde_json::to_string(&channel_ids)?;
        self.client.set(MEMBER_CHANNEL_IDS_KEY, &payload, ttl).await
    }

    #[allow(clippy::needless_pass_by_value)]
    fn remember_member(&self, member: Member) {
        if let Ok(mut memory) = self.memory.write() {
            if !member.channel_id.is_empty() {
                memory
                    .by_channel_id
                    .insert(member.channel_id.clone(), member.clone());
                if !memory.channel_ids.contains(&member.channel_id) {
                    memory.channel_ids.push(member.channel_id.clone());
                }
            }

            memory
                .by_name
                .insert(normalize_key(&member.name), member.clone());
            if let Some(english_name) = &member.english_name {
                memory
                    .by_name
                    .insert(normalize_key(english_name), member.clone());
            }

            for alias in member.all_aliases() {
                memory
                    .alias_to_channel
                    .insert(normalize_key(&alias), member.channel_id.clone());
            }
        }
    }

    async fn cache_member(
        &self,
        member: &Member,
        ttl: Option<Duration>,
    ) -> Result<(), SharedError> {
        let payload = serde_json::to_string(member)?;

        if !member.channel_id.is_empty() {
            let key = format!("{MEMBER_CHANNEL_KEY_PREFIX}{}", member.channel_id);
            self.client.set(&key, &payload, ttl).await?;
        }

        let name_key = format!("{MEMBER_NAME_KEY_PREFIX}{}", normalize_key(&member.name));
        self.client.set(&name_key, &payload, ttl).await?;

        if let Some(english_name) = &member.english_name {
            let english_key = format!("{MEMBER_NAME_KEY_PREFIX}{}", normalize_key(english_name));
            self.client.set(&english_key, &payload, ttl).await?;
        }

        for alias in member.all_aliases() {
            let alias_key = format!("{MEMBER_ALIAS_KEY_PREFIX}{}", normalize_key(&alias));
            self.client.set(&alias_key, &payload, ttl).await?;
        }

        Ok(())
    }

    async fn fetch_member(&self, key: &str) -> Result<Option<Member>, SharedError> {
        let Some(payload) = self.client.get(key).await? else {
            return Ok(None);
        };

        let member = serde_json::from_str::<Member>(&payload)?;
        self.remember_member(member.clone());
        Ok(Some(member))
    }
}

#[async_trait]
impl MemberDataProvider for MemberCache {
    async fn get_by_channel_id(&self, channel_id: &str) -> Result<Option<Member>, SharedError> {
        if let Ok(memory) = self.memory.read()
            && let Some(member) = memory.by_channel_id.get(channel_id)
        {
            return Ok(Some(member.clone()));
        }

        let key = format!("{MEMBER_CHANNEL_KEY_PREFIX}{channel_id}");
        self.fetch_member(&key).await
    }

    async fn get_by_name(&self, name: &str) -> Result<Option<Member>, SharedError> {
        let normalized_name = normalize_key(name);

        if let Ok(memory) = self.memory.read()
            && let Some(member) = memory.by_name.get(&normalized_name)
        {
            return Ok(Some(member.clone()));
        }

        let key = format!("{MEMBER_NAME_KEY_PREFIX}{normalized_name}");
        self.fetch_member(&key).await
    }

    async fn find_by_alias(&self, alias: &str) -> Result<Option<Member>, SharedError> {
        let normalized_alias = normalize_key(alias);

        if let Ok(memory) = self.memory.read()
            && let Some(channel_id) = memory.alias_to_channel.get(&normalized_alias)
            && let Some(member) = memory.by_channel_id.get(channel_id)
        {
            return Ok(Some(member.clone()));
        }

        let key = format!("{MEMBER_ALIAS_KEY_PREFIX}{normalized_alias}");
        if let Some(member) = self.fetch_member(&key).await? {
            return Ok(Some(member));
        }

        let channel_ids = self.get_all_channel_ids().await?;
        for channel_id in channel_ids {
            if let Some(member) = self.get_by_channel_id(&channel_id).await?
                && member
                    .all_aliases()
                    .into_iter()
                    .any(|candidate| normalize_key(&candidate) == normalized_alias)
            {
                return Ok(Some(member));
            }
        }

        Ok(None)
    }

    async fn get_all_channel_ids(&self) -> Result<Vec<String>, SharedError> {
        if let Ok(memory) = self.memory.read()
            && !memory.channel_ids.is_empty()
        {
            return Ok(memory.channel_ids.clone());
        }

        if let Some(payload) = self.client.get(MEMBER_CHANNEL_IDS_KEY).await? {
            let channel_ids = serde_json::from_str::<Vec<String>>(&payload)?;
            if let Ok(mut memory) = self.memory.write() {
                memory.channel_ids.clone_from(&channel_ids);
            }
            return Ok(channel_ids);
        }

        Ok(Vec::new())
    }
}
