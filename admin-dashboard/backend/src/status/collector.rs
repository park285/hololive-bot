use anyhow::Context;
use reqwest::Client;
use serde::Serialize;
use std::time::{Duration, Instant};
use utoipa::ToSchema;

#[derive(Debug, Clone, Serialize)]
pub struct ServiceEndpoint {
    pub name: String,
    pub url: String,
    pub health_path: String,
}

#[derive(Debug, Clone, Serialize, ToSchema)]
pub struct ServiceStatus {
    pub name: String,
    pub available: bool,
    pub response_time_ms: Option<u64>,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Serialize, ToSchema)]
pub struct AggregatedStatus {
    pub services: Vec<ServiceStatus>,
    pub uptime: String,
    pub version: String,
}

#[allow(missing_debug_implementations)]
pub struct StatusCollector {
    http_client: Client,
    endpoints: Vec<ServiceEndpoint>,
    start_time: Instant,
    version: String,
}

impl StatusCollector {
    pub fn new(endpoints: Vec<ServiceEndpoint>, version: &str) -> anyhow::Result<Self> {
        let http_client = Client::builder()
            .timeout(Duration::from_secs(3))
            .build()
            .context("build status collector http client")?;

        Ok(Self {
            http_client,
            endpoints,
            start_time: Instant::now(),
            version: version.to_string(),
        })
    }

    pub async fn collect(&self) -> AggregatedStatus {
        let mut services = Vec::with_capacity(self.endpoints.len() + 1);
        services.push(ServiceStatus {
            name: "admin-dashboard".to_string(),
            available: true,
            response_time_ms: Some(0),
            error: None,
        });

        let futures: Vec<_> = self
            .endpoints
            .iter()
            .map(|ep| {
                let client = self.http_client.clone();
                let url = format!("{}{}", ep.url, ep.health_path);
                let name = ep.name.clone();

                async move {
                    let start = Instant::now();
                    match client.get(&url).send().await {
                        Ok(resp) if resp.status().is_success() => ServiceStatus {
                            name,
                            available: true,
                            response_time_ms: Some(start.elapsed().as_millis() as u64),
                            error: None,
                        },
                        Ok(resp) => ServiceStatus {
                            name,
                            available: false,
                            response_time_ms: Some(start.elapsed().as_millis() as u64),
                            error: Some(format!("status: {}", resp.status())),
                        },
                        Err(err) => ServiceStatus {
                            name,
                            available: false,
                            response_time_ms: None,
                            error: Some(err.to_string()),
                        },
                    }
                }
            })
            .collect();

        let results = futures::future::join_all(futures).await;
        services.extend(results);

        AggregatedStatus {
            services,
            uptime: format_duration(self.start_time.elapsed()),
            version: self.version.clone(),
        }
    }
}

pub fn format_duration(duration: Duration) -> String {
    let secs = duration.as_secs();
    let days = secs / 86_400;
    let hours = (secs % 86_400) / 3_600;
    let mins = (secs % 3_600) / 60;

    if days > 0 {
        format!("{days}d {hours}h {mins}m")
    } else if hours > 0 {
        format!("{hours}h {mins}m")
    } else {
        format!("{mins}m")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_format_duration_minutes() {
        assert_eq!(format_duration(Duration::from_secs(300)), "5m");
    }

    #[test]
    fn test_format_duration_hours() {
        assert_eq!(format_duration(Duration::from_secs(7_200)), "2h 0m");
    }

    #[test]
    fn test_format_duration_days() {
        assert_eq!(format_duration(Duration::from_secs(90_000)), "1d 1h 0m");
    }

    #[tokio::test]
    async fn test_collect_no_endpoints_has_self() {
        let collector = StatusCollector::new(vec![], "1.0.0").expect("status collector init");
        let status = collector.collect().await;

        assert_eq!(status.services.len(), 1);
        assert_eq!(status.services[0].name, "admin-dashboard");
        assert!(status.services[0].available);
        assert_eq!(status.version, "1.0.0");
    }

    #[tokio::test]
    async fn test_collect_unreachable_endpoint() {
        let endpoints = vec![ServiceEndpoint {
            name: "test-svc".to_string(),
            url: "http://localhost:1".to_string(),
            health_path: "/health".to_string(),
        }];
        let collector = StatusCollector::new(endpoints, "1.0.0").expect("status collector init");
        let status = collector.collect().await;

        assert_eq!(status.services.len(), 2);
        let test_svc = &status.services[1];
        assert!(!test_svc.available);
        assert!(test_svc.error.is_some());
    }
}
