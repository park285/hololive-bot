use std::sync::Arc;

use crate::auth::rate_limiter::LoginRateLimiter;
use crate::auth::session::ValkeySessionStore;
use crate::config::Config;
use crate::docker::DockerService;
use crate::proxy::BotProxy;
use crate::stream_limiter::StreamLimiter;

pub struct AppState {
    pub config: Config,
    pub sessions: ValkeySessionStore,
    pub rate_limiter: Arc<LoginRateLimiter>,
    pub bot_proxy: Option<BotProxy>,
    pub docker_svc: Option<Arc<DockerService>>,
    pub stream_limiter: Arc<StreamLimiter>,
}
