use clap::Parser;
use std::path::PathBuf;

#[derive(Debug, Parser)]
#[command(name = "alarm-app")]
#[command(about = "hololive-alarm service")]
pub struct Cli {
    #[arg(long, env = "ALARM_CONFIG_PATH", default_value = "alarm-config.toml")]
    pub config: PathBuf,
}
