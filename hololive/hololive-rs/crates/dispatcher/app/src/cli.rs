use std::path::PathBuf;

use clap::Parser;

#[derive(Debug, Parser)]
#[command(author, version, about = "Alarm dispatcher app")]
pub(crate) struct Cli {
    #[arg(long, default_value = "dispatcher-config.toml")]
    pub config: PathBuf,
}
