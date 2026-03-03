use alarm_core::{error::AlarmError, model::Stream};
use async_trait::async_trait;

#[async_trait]
pub trait HolodexClient: Send + Sync {
    async fn get_live_streams(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError>;
    async fn get_channel_streams(&self, channel_id: &str) -> Result<Vec<Stream>, AlarmError>;
}

#[async_trait]
pub trait ChzzkClient: Send + Sync {
    async fn get_live_status(&self, channel_id: &str) -> Result<Option<Stream>, AlarmError>;
}

#[async_trait]
pub trait TwitchClient: Send + Sync {
    async fn get_streams(&self, user_logins: &[&str]) -> Result<Vec<Stream>, AlarmError>;
}

#[cfg(test)]
pub struct MockHolodexClient {
    streams: Vec<Stream>,
}

#[cfg(test)]
impl MockHolodexClient {
    pub fn new(streams: Vec<Stream>) -> Self {
        Self { streams }
    }
}

#[cfg(test)]
#[async_trait]
impl HolodexClient for MockHolodexClient {
    async fn get_live_streams(&self, channel_ids: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        Ok(self
            .streams
            .iter()
            .filter(|stream| channel_ids.contains(&stream.channel_id.as_str()))
            .cloned()
            .collect())
    }

    async fn get_channel_streams(&self, channel_id: &str) -> Result<Vec<Stream>, AlarmError> {
        Ok(self
            .streams
            .iter()
            .filter(|stream| stream.channel_id == channel_id)
            .cloned()
            .collect())
    }
}

#[cfg(test)]
pub struct MockChzzkClient {
    live_stream: Option<Stream>,
    error: Option<String>,
}

#[cfg(test)]
impl MockChzzkClient {
    pub fn new(live_stream: Option<Stream>) -> Self {
        Self {
            live_stream,
            error: None,
        }
    }

    pub fn with_error(message: impl Into<String>) -> Self {
        Self {
            live_stream: None,
            error: Some(message.into()),
        }
    }
}

#[cfg(test)]
#[async_trait]
impl ChzzkClient for MockChzzkClient {
    async fn get_live_status(&self, _channel_id: &str) -> Result<Option<Stream>, AlarmError> {
        if let Some(message) = &self.error {
            return Err(AlarmError::Http(message.clone()));
        }
        Ok(self.live_stream.clone())
    }
}

#[cfg(test)]
pub struct MockTwitchClient {
    streams: Vec<Stream>,
    error: Option<String>,
}

#[cfg(test)]
impl MockTwitchClient {
    pub fn new(streams: Vec<Stream>) -> Self {
        Self {
            streams,
            error: None,
        }
    }

    pub fn with_error(message: impl Into<String>) -> Self {
        Self {
            streams: vec![],
            error: Some(message.into()),
        }
    }
}

#[cfg(test)]
#[async_trait]
impl TwitchClient for MockTwitchClient {
    async fn get_streams(&self, user_logins: &[&str]) -> Result<Vec<Stream>, AlarmError> {
        if let Some(message) = &self.error {
            return Err(AlarmError::Http(message.clone()));
        }

        Ok(self
            .streams
            .iter()
            .filter(|stream| user_logins.contains(&stream.twitch_user_login.as_str()))
            .cloned()
            .collect())
    }
}
