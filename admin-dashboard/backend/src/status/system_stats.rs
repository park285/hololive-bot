use reqwest::Client;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::time::Duration;
use sysinfo::{ProcessesToUpdate, System, get_current_pid};
use tokio::sync::broadcast;
use tokio::time::MissedTickBehavior;
use tokio_util::sync::CancellationToken;

use super::ServiceEndpoint;

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ServiceRuntimeStats {
    pub name: String,
    pub goroutines: usize,
    pub available: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SystemStats {
    pub cpu_usage: f32,
    pub memory_total: u64,
    pub memory_used: u64,
    pub memory_usage: f32,
    pub goroutines: usize,
    pub total_goroutines: usize,
    pub service_goroutines: Vec<ServiceRuntimeStats>,
    pub load_avg_1: f64,
    pub load_avg_5: f64,
    pub load_avg_15: f64,
}

#[allow(missing_debug_implementations)]
pub struct SystemStatsCollector;

impl SystemStatsCollector {
    pub fn start(
        tx: broadcast::Sender<SystemStats>,
        endpoints: Vec<ServiceEndpoint>,
        cancel: CancellationToken,
    ) {
        tokio::spawn(async move {
            let mut sys = System::new();
            let current_pid = get_current_pid().ok();
            let http_client = Client::builder()
                .timeout(Duration::from_secs(2))
                .build()
                .expect("system stats http client");
            let mut ticker = tokio::time::interval(Duration::from_secs(2));
            ticker.set_missed_tick_behavior(MissedTickBehavior::Delay);

            loop {
                tokio::select! {
                    () = cancel.cancelled() => break,
                    _ = ticker.tick() => {
                        sys.refresh_cpu_all();
                        sys.refresh_memory();
                        if let Some(pid) = current_pid {
                            let _ = sys.refresh_processes(ProcessesToUpdate::Some(&[pid]), false);
                        }

                        let load = System::load_average();
                        let total_memory = sys.total_memory();
                        let used_memory = sys.used_memory();
                        let goroutines = current_pid
                            .and_then(|pid| sys.process(pid))
                            .and_then(|process| process.tasks())
                            .map_or(0, std::collections::HashSet::len);
                        let external_service_goroutines =
                            fetch_service_runtime_stats(&http_client, &endpoints).await;
                        let total_goroutines = goroutines
                            + external_service_goroutines
                                .iter()
                                .filter(|service| service.available)
                                .map(|service| service.goroutines)
                                .sum::<usize>();
                        let mut service_goroutines =
                            Vec::with_capacity(external_service_goroutines.len() + 1);
                        service_goroutines.push(ServiceRuntimeStats {
                            name: "admin-dashboard".to_string(),
                            goroutines,
                            available: true,
                        });
                        service_goroutines.extend(external_service_goroutines);
                        let stats = SystemStats {
                            cpu_usage: sys.global_cpu_usage(),
                            memory_total: total_memory,
                            memory_used: used_memory,
                            memory_usage: if total_memory > 0 {
                                (used_memory as f32 / total_memory as f32) * 100.0
                            } else {
                                0.0
                            },
                            goroutines,
                            total_goroutines,
                            service_goroutines,
                            load_avg_1: load.one,
                            load_avg_5: load.five,
                            load_avg_15: load.fifteen,
                        };
                        let _ = tx.send(stats);
                    }
                }
            }
        });
    }
}

#[derive(Debug, Deserialize)]
struct HealthResponse {
    #[serde(default)]
    goroutines: usize,
    #[serde(default)]
    components: HashMap<String, HealthComponent>,
}

#[derive(Debug, Deserialize, Default)]
struct HealthComponent {
    #[serde(default)]
    detail: HashMap<String, serde_json::Value>,
}

async fn fetch_service_runtime_stats(
    client: &Client,
    endpoints: &[ServiceEndpoint],
) -> Vec<ServiceRuntimeStats> {
    let mut services = Vec::with_capacity(endpoints.len());

    for endpoint in endpoints {
        services.push(fetch_service_runtime_stat(client, endpoint).await);
    }

    services
}

async fn fetch_service_runtime_stat(
    client: &Client,
    endpoint: &ServiceEndpoint,
) -> ServiceRuntimeStats {
    let url = format!("{}{}", endpoint.url, endpoint.health_path);
    let response = client.get(&url).send().await;

    match response {
        Ok(resp) if resp.status().is_success() => match resp.json::<HealthResponse>().await {
            Ok(health) => ServiceRuntimeStats {
                name: endpoint.name.clone(),
                goroutines: extract_goroutines(&health),
                available: true,
            },
            Err(_) => ServiceRuntimeStats {
                name: endpoint.name.clone(),
                goroutines: 0,
                available: true,
            },
        },
        _ => ServiceRuntimeStats {
            name: endpoint.name.clone(),
            goroutines: 0,
            available: false,
        },
    }
}

fn extract_goroutines(health: &HealthResponse) -> usize {
    if health.goroutines > 0 {
        return health.goroutines;
    }

    health
        .components
        .get("app")
        .and_then(|component| component.detail.get("goroutines"))
        .and_then(serde_json::Value::as_u64)
        .map_or(0, |value| value as usize)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use tokio::sync::broadcast;

    #[test]
    fn test_extract_goroutines_prefers_top_level_field() {
        let health = HealthResponse {
            goroutines: 42,
            components: HashMap::new(),
        };

        assert_eq!(extract_goroutines(&health), 42);
    }

    #[test]
    fn test_extract_goroutines_supports_nested_app_detail() {
        let mut detail = HashMap::new();
        detail.insert("goroutines".to_string(), json!(30));
        let mut components = HashMap::new();
        components.insert("app".to_string(), HealthComponent { detail });

        let health = HealthResponse {
            goroutines: 0,
            components,
        };

        assert_eq!(extract_goroutines(&health), 30);
    }

    #[test]
    fn test_system_stats_serializes_frontend_contract() {
        let stats = SystemStats {
            cpu_usage: 12.5,
            memory_total: 1024,
            memory_used: 256,
            memory_usage: 25.0,
            goroutines: 7,
            total_goroutines: 7,
            service_goroutines: vec![ServiceRuntimeStats {
                name: "admin-dashboard".to_string(),
                goroutines: 7,
                available: true,
            }],
            load_avg_1: 0.1,
            load_avg_5: 0.2,
            load_avg_15: 0.3,
        };

        let value = serde_json::to_value(stats).unwrap();

        assert_eq!(value["cpuUsage"], json!(12.5));
        assert_eq!(value["memoryTotal"], json!(1024));
        assert_eq!(value["memoryUsed"], json!(256));
        assert_eq!(value["memoryUsage"], json!(25.0));
        assert_eq!(value["goroutines"], json!(7));
        assert_eq!(value["totalGoroutines"], json!(7));
        assert_eq!(
            value["serviceGoroutines"][0]["name"],
            json!("admin-dashboard")
        );
        assert!(value.get("cpu_usage").is_none());
        assert!(value.get("memory_usage_percent").is_none());
    }

    #[tokio::test]
    async fn test_broadcast_receiver_gets_stats() {
        let (tx, mut rx) = broadcast::channel(16);
        let cancel = CancellationToken::new();
        SystemStatsCollector::start(tx, vec![], cancel.clone());

        let stats = tokio::time::timeout(std::time::Duration::from_secs(5), rx.recv()).await;

        cancel.cancel();

        assert!(stats.is_ok());
        let stats = stats.unwrap().unwrap();
        assert!(stats.memory_total > 0);
        assert_eq!(stats.service_goroutines[0].name, "admin-dashboard");
    }
}
