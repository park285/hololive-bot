mod config;
mod error;

fn main() {
    dotenvy::dotenv().ok();
    let _cfg = config::Config::load();
    println!("config loaded");
}
