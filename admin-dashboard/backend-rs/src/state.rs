// TODO: move to state.rs in Task 19 -- 현재는 컴파일용 최소 placeholder
use crate::auth::session::ValkeySessionStore;
use crate::config::Config;

pub struct AppState {
    pub config: Config,
    pub sessions: ValkeySessionStore,
}
