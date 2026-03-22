mod auth;
mod config;
mod error;
mod middleware;
mod state;
mod stream_limiter;

fn main() {
    dotenvy::dotenv().ok();
    let _cfg = config::Config::load();
    println!("config loaded");
}
