use thiserror::Error;

#[derive(Error, Debug)]
pub enum ScraperError {
    #[error("HTTP request failed: {0}")]
    Http(String),

    #[error("HTTP status {code}: {message}")]
    HttpStatus { code: u16, message: String },

    #[error("XML parse failed: {0}")]
    XmlParse(String),

    #[error("Database error: {0}")]
    Database(String),

    #[error("Config error: {0}")]
    Config(String),

    #[error("All feeds failed: {0}")]
    AllFeedsFailed(String),

    #[error("Link check blocked: {0}")]
    LinkBlocked(String),

    #[error("Link check failed: {0}")]
    LinkFailed(String),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),
}

impl ScraperError {
    pub fn is_retryable(&self) -> bool {
        match self {
            Self::HttpStatus { code, .. } => matches!(code, 502..=504),
            Self::Http(msg) => {
                let lower = msg.to_lowercase();
                is_transient_signature(&lower)
            }
            _ => false,
        }
    }
}

fn is_transient_signature(msg: &str) -> bool {
    const PATTERNS: &[&str] = &[
        "connection reset by peer",
        "connection reset",
        "connection refused",
        "broken pipe",
        "http2: timeout awaiting response headers",
        "timeout exceeded while awaiting headers",
        "client.timeout exceeded while awaiting headers",
        "client.timeout exceeded",
        "unexpected eof",
        "timed out",
        "connect error",
    ];

    PATTERNS.iter().any(|pattern| msg.contains(pattern))
}
