use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Talent {
    pub japanese: String,
    pub english: String,
    pub link: String,
    pub status: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TalentProfile {
    pub slug: String,
    pub english_name: String,
    pub japanese_name: String,
    pub catchphrase: String,
    pub description: String,
    pub data_entries: Vec<TalentProfileEntry>,
    pub social_links: Vec<TalentSocialLink>,
    pub official_url: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TalentProfileEntry {
    pub label: String,
    pub value: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TalentSocialLink {
    pub label: String,
    pub url: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Translated {
    pub display_name: String,
    pub catchphrase: String,
    pub summary: String,
    pub highlights: Vec<String>,
    pub data: Vec<TranslatedProfileDataRow>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TranslatedProfileDataRow {
    pub label: String,
    pub value: String,
}
