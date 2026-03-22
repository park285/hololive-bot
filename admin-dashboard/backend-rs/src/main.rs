mod auth;
mod config;
mod docker;
mod error;
mod handlers;
mod middleware;
mod proxy;
mod ssr;
mod state;
mod static_files;
mod status;
mod stream_limiter;

fn main() {
    dotenvy::dotenv().ok();
    let _cfg = config::Config::load();
    println!("config loaded");
}
