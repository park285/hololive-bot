pub mod bot_proxy;
pub use bot_proxy::BotProxy;

pub const HOLO_PROXY_ROUTE: &str = "/admin/api/holo/{*path}";
pub const HOLO_PROXY_PREFIX: &str = "/admin/api/holo";
pub const HOLO_UPSTREAM_PREFIX: &str = "/api/holo";
