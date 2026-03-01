mod cache;
mod matcher;

use async_trait::async_trait;
pub use cache::MemberCache;
pub use matcher::DefaultMemberMatcher;
use shared_core::{error::SharedError, model::member::Member};

#[async_trait]
pub trait MemberDataProvider: Send + Sync {
    async fn get_by_channel_id(&self, channel_id: &str) -> Result<Option<Member>, SharedError>;
    async fn get_by_name(&self, name: &str) -> Result<Option<Member>, SharedError>;
    async fn find_by_alias(&self, alias: &str) -> Result<Option<Member>, SharedError>;
    async fn get_all_channel_ids(&self) -> Result<Vec<String>, SharedError>;
}

pub trait MemberMatcher: Send + Sync {
    fn best_match(&self, query: &str) -> Option<Member>;
    fn find_candidates(&self, query: &str, limit: usize) -> Vec<Member>;
}

pub(crate) fn normalize_key(raw: &str) -> String {
    raw.trim().to_lowercase().replace(char::is_whitespace, "")
}
