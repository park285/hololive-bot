use std::sync::RwLock;
use std::time::{Duration, Instant};

use async_trait::async_trait;
use bollard::Docker;
use bollard::models::ContainerSummary;
use bollard::query_parameters::{
    ListContainersOptions, RestartContainerOptions, StopContainerOptions,
};

use crate::docker::{Container, DockerProvider, PortMapping};
use crate::error::DockerError;

#[allow(missing_debug_implementations)]
pub struct DockerService {
    docker: Docker,
    managed_prefixes: Vec<String>,
    exclude_suffixes: Vec<String>,
    cache: RwLock<Option<(Instant, Vec<Container>)>>,
    cache_ttl: Duration,
}

impl DockerService {
    pub fn new(docker_host: &str) -> Result<Self, DockerError> {
        let docker = Docker::connect_with_http(docker_host, 10, bollard::API_DEFAULT_VERSION)
            .map_err(|e| DockerError::Internal(e.to_string()))?;
        Ok(Self {
            docker,
            managed_prefixes: vec![
                "hololive".into(),
                "valkey".into(),
                "postgres".into(),
                "deunhealth".into(),
                "admin".into(),
            ],
            exclude_suffixes: vec!["-init".into()],
            cache: RwLock::new(None),
            cache_ttl: Duration::from_secs(5),
        })
    }

    #[cfg(test)]
    pub fn new_test() -> Self {
        Self {
            docker: Docker::connect_with_http("tcp://localhost:0", 1, bollard::API_DEFAULT_VERSION)
                .unwrap(),
            managed_prefixes: vec![
                "hololive".into(),
                "valkey".into(),
                "postgres".into(),
                "deunhealth".into(),
                "admin".into(),
            ],
            exclude_suffixes: vec!["-init".into()],
            cache: RwLock::new(None),
            cache_ttl: Duration::from_secs(5),
        }
    }

    fn clear_cache(&self) {
        if let Ok(mut cache) = self.cache.write() {
            *cache = None;
        }
    }
}

#[async_trait]
impl DockerProvider for DockerService {
    async fn available(&self) -> bool {
        self.docker.ping().await.is_ok()
    }

    async fn list_containers(&self) -> Result<Vec<Container>, DockerError> {
        if let Ok(cache) = self.cache.read()
            && let Some((created_at, containers)) = cache.as_ref()
            && created_at.elapsed() < self.cache_ttl
        {
            return Ok(containers.clone());
        }

        let containers = self
            .docker
            .list_containers(Some(ListContainersOptions {
                all: true,
                ..Default::default()
            }))
            .await
            .map_err(|e| DockerError::Internal(e.to_string()))?;

        let mut mapped = containers
            .into_iter()
            .filter_map(|container| self.map_container(container))
            .collect::<Vec<_>>();

        mapped.sort_by(|a, b| a.name.cmp(&b.name));

        if let Ok(mut cache) = self.cache.write() {
            *cache = Some((Instant::now(), mapped.clone()));
        }

        Ok(mapped)
    }

    async fn restart_container(&self, name: &str) -> Result<(), DockerError> {
        if !self.is_managed(name) {
            return Err(DockerError::NotManaged(name.to_string()));
        }

        self.docker
            .restart_container(
                name,
                Some(RestartContainerOptions {
                    signal: None,
                    t: Some(30),
                }),
            )
            .await
            .map_err(|e| DockerError::Internal(e.to_string()))?;

        self.clear_cache();
        Ok(())
    }

    async fn stop_container(&self, name: &str) -> Result<(), DockerError> {
        if !self.is_managed(name) {
            return Err(DockerError::NotManaged(name.to_string()));
        }

        self.docker
            .stop_container(
                name,
                Some(StopContainerOptions {
                    signal: None,
                    t: Some(30),
                }),
            )
            .await
            .map_err(|e| DockerError::Internal(e.to_string()))?;

        self.clear_cache();
        Ok(())
    }

    async fn start_container(&self, name: &str) -> Result<(), DockerError> {
        if !self.is_managed(name) {
            return Err(DockerError::NotManaged(name.to_string()));
        }

        self.docker
            .start_container(
                name,
                None::<bollard::query_parameters::StartContainerOptions>,
            )
            .await
            .map_err(|e| DockerError::Internal(e.to_string()))?;

        self.clear_cache();
        Ok(())
    }

    fn is_managed(&self, name: &str) -> bool {
        self.managed_prefixes
            .iter()
            .any(|prefix| name.starts_with(prefix))
            && !self
                .exclude_suffixes
                .iter()
                .any(|suffix| name.ends_with(suffix))
    }
}

impl DockerService {
    fn map_container(&self, container: ContainerSummary) -> Option<Container> {
        let name = container
            .names
            .as_ref()
            .and_then(|names| names.first())
            .map(|name| name.trim_start_matches('/').to_string())?;

        if !self.is_managed(&name) {
            return None;
        }

        let ports = container
            .ports
            .unwrap_or_default()
            .into_iter()
            .map(|port| PortMapping {
                private_port: port.private_port,
                public_port: port.public_port,
                port_type: port
                    .typ
                    .map_or_else(|| "tcp".to_string(), |t| t.to_string()),
            })
            .collect();

        let status = container.status.unwrap_or_default();
        let health = parse_health(&status);

        Some(Container {
            id: container.id.unwrap_or_default(),
            name,
            image: container.image.unwrap_or_default(),
            status,
            state: container
                .state
                .map(|state| state.to_string())
                .unwrap_or_default(),
            health,
            created: container.created.unwrap_or_default(),
            ports,
        })
    }
}

fn parse_health(status: &str) -> Option<String> {
    ["healthy", "unhealthy", "starting"]
        .into_iter()
        .find(|health| status.contains(&format!("({health})")))
        .map(str::to_string)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_managed() {
        let svc = DockerService::new_test();
        assert!(svc.is_managed("hololive-kakao-bot-go"));
        assert!(svc.is_managed("valkey-cache"));
        assert!(svc.is_managed("admin-dashboard"));
        assert!(!svc.is_managed("random-container"));
        assert!(!svc.is_managed("hololive-init"));
    }

    #[test]
    fn test_is_managed_exclude_suffix() {
        let svc = DockerService::new_test();
        assert!(!svc.is_managed("hololive-bot-init"));
        assert!(svc.is_managed("hololive-bot"));
    }

    #[test]
    fn test_parse_health_from_status() {
        assert_eq!(
            parse_health("Up 2 hours (healthy)"),
            Some("healthy".to_string())
        );
        assert_eq!(
            parse_health("Up 5 minutes (unhealthy)"),
            Some("unhealthy".to_string())
        );
        assert_eq!(
            parse_health("Up 1 minute (starting)"),
            Some("starting".to_string())
        );
        assert_eq!(parse_health("Up 3 hours"), None);
        assert_eq!(parse_health("Exited (0) 2 hours ago"), None);
    }
}
