use async_trait::async_trait;
use serde::Serialize;

pub mod service;

pub use service::DockerService;

#[derive(Debug, Clone, Serialize)]
pub struct Container {
    pub id: String,
    pub name: String,
    pub image: String,
    pub status: String,
    pub state: String,
    pub health: Option<String>,
    pub created: i64,
    pub ports: Vec<PortMapping>,
}

#[derive(Debug, Clone, Serialize)]
pub struct PortMapping {
    pub private_port: u16,
    pub public_port: Option<u16>,
    pub port_type: String,
}

#[async_trait]
pub trait DockerProvider: Send + Sync {
    async fn available(&self) -> bool;
    async fn list_containers(&self) -> Result<Vec<Container>, crate::error::DockerError>;
    async fn restart_container(&self, name: &str) -> Result<(), crate::error::DockerError>;
    async fn stop_container(&self, name: &str) -> Result<(), crate::error::DockerError>;
    async fn start_container(&self, name: &str) -> Result<(), crate::error::DockerError>;
    async fn get_log_stream(
        &self,
        name: &str,
    ) -> Result<
        std::pin::Pin<Box<dyn tokio::io::AsyncRead + Send + Unpin>>,
        crate::error::DockerError,
    >;
    fn is_managed(&self, name: &str) -> bool;
}
