use thiserror::Error;

#[derive(Error, Debug)]
pub enum SharedError {
    #[error("http: {0}")]
    Http(String),

    #[error("http status {code}: {message}")]
    HttpStatus { code: u16, message: String },

    #[error("database: {0}")]
    Database(String),

    #[error("valkey: {0}")]
    Valkey(String),

    #[error("config: {0}")]
    Config(String),

    #[error("serialization: {0}")]
    Serialization(#[from] serde_json::Error),

    #[error("api ({platform}): {message}")]
    Api { platform: String, message: String },

    #[error("circuit open: {platform}")]
    CircuitOpen { platform: String },

    #[error("not found: {0}")]
    NotFound(String),

    #[error("unauthorized: {0}")]
    Unauthorized(String),

    #[error("forbidden: {0}")]
    Forbidden(String),

    #[error("conflict: {0}")]
    Conflict(String),

    #[error("io: {0}")]
    Io(#[from] std::io::Error),
}
