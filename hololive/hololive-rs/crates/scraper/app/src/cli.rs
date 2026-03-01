use std::path::PathBuf;

use clap::Parser;

#[derive(Debug, Parser)]
#[command(name = "scraper-app")]
#[command(about = "hololive-rs service")]
pub struct Cli {
    #[arg(long, env = "SCRAPER_CONFIG_PATH", default_value = "config.toml")]
    pub config: PathBuf,

    #[arg(long, env = "SCRAPER_RUN_ONCE", action = clap::ArgAction::SetTrue)]
    pub run_once: bool,
}
