use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Aliases {
    pub ko: Vec<String>,
    pub ja: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Member {
    pub channel_id: String,
    pub name: String,
    pub english_name: Option<String>,
    pub aliases: Option<Aliases>,
    pub org: Option<String>,
    pub suborg: Option<String>,
    pub chzzk_channel_id: Option<String>,
    pub twitch_user_id: Option<String>,
}

impl Member {
    pub fn all_aliases(&self) -> Vec<String> {
        let mut aliases = Vec::new();
        if let Some(alias) = &self.aliases {
            aliases.extend(alias.ko.clone());
            aliases.extend(alias.ja.clone());
        }
        aliases
    }
}
