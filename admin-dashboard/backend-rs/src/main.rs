mod auth;
mod config;
mod error;
mod middleware;
mod state;

fn main() {
    dotenvy::dotenv().ok();
    let _cfg = config::Config::load();
    println!("config loaded");
}
