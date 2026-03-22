use std::sync::Arc;

use crate::auth::rate_limiter::LoginRateLimiter;
use crate::auth::session::ValkeySessionStore;
use crate::config::Config;
use crate::proxy::BotProxy;

pub struct AppState {
    pub config: Config,
    pub sessions: ValkeySessionStore,
    pub rate_limiter: Arc<LoginRateLimiter>,
    pub bot_proxy: Option<BotProxy>,
}
