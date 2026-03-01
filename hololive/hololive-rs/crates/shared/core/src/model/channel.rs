use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Channel {
    pub id: String,
    pub name: String,
    pub english_name: Option<String>,
    pub photo: Option<String>,
    pub twitter: Option<String>,
    pub video_count: Option<i32>,
    pub subscriber_count: Option<i32>,
    pub org: Option<String>,
    pub suborg: Option<String>,
    pub group: Option<String>,
}

impl Channel {
    pub fn display_name(&self) -> &str {
        if let Some(english_name) = &self.english_name
            && !english_name.is_empty()
        {
            return english_name.as_str();
        }
        &self.name
    }

    pub fn is_hololive(&self) -> bool {
        self.org.as_deref() == Some("Hololive")
    }

    pub fn has_photo(&self) -> bool {
        self.photo.as_deref().is_some_and(|photo| !photo.is_empty())
    }

    pub fn photo_url(&self) -> &str {
        if self.has_photo() {
            self.photo.as_deref().unwrap_or("")
        } else {
            ""
        }
    }
}
