use std::time::Duration;

use sea_orm::{ConnectOptions, Database, DatabaseConnection};
use secrecy::{ExposeSecret, SecretString};
use serde::Deserialize;
use shared_core::error::SharedError;
use validator::Validate;

#[derive(Debug, Clone, Deserialize, Validate)]
pub struct DbConfig {
    #[validate(length(min = 1))]
    pub host: String,
    #[validate(range(min = 1, max = 65535))]
    pub port: u16,
    #[validate(length(min = 1))]
    pub user: String,
    pub password: SecretString,
    #[validate(length(min = 1))]
    pub database: String,
    #[validate(range(min = 1))]
    pub max_connections: u32,
    pub min_connections: u32,
    #[validate(range(min = 1))]
    pub connect_timeout_secs: u64,
    #[validate(range(min = 1))]
    pub idle_timeout_secs: u64,
    #[validate(range(min = 1))]
    pub max_lifetime_secs: u64,
}

impl Default for DbConfig {
    fn default() -> Self {
        Self {
            host: "postgres".to_owned(),
            port: 5432,
            user: "hololive_runtime".to_owned(),
            password: SecretString::from(""),
            database: "hololive".to_owned(),
            max_connections: 25,
            min_connections: 5,
            connect_timeout_secs: 10,
            idle_timeout_secs: 300,
            max_lifetime_secs: 3600,
        }
    }
}

pub async fn create_pool(config: &DbConfig) -> Result<DatabaseConnection, SharedError> {
    let encoded_user: String =
        url::form_urlencoded::byte_serialize(config.user.as_bytes()).collect();
    let encoded_password: String =
        url::form_urlencoded::byte_serialize(config.password.expose_secret().as_bytes()).collect();
    let encoded_database: String =
        url::form_urlencoded::byte_serialize(config.database.as_bytes()).collect();

    let dsn = format!(
        "postgres://{}:{}@{}:{}/{}",
        encoded_user, encoded_password, config.host, config.port, encoded_database
    );

    let mut options = ConnectOptions::new(dsn);
    options
        .max_connections(config.max_connections)
        .min_connections(config.min_connections)
        .connect_timeout(Duration::from_secs(config.connect_timeout_secs))
        .idle_timeout(Duration::from_secs(config.idle_timeout_secs))
        .max_lifetime(Duration::from_secs(config.max_lifetime_secs));

    Database::connect(options)
        .await
        .map_err(|e| SharedError::Database(format!("create database pool: {e}")))
}

pub async fn health_check(db: &DatabaseConnection) -> Result<(), SharedError> {
    db.ping()
        .await
        .map_err(|e| SharedError::Database(format!("database health check failed: {e}")))
}
