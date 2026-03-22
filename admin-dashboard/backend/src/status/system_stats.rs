use serde::Serialize;
use sysinfo::System;
use tokio::sync::broadcast;
use tokio_util::sync::CancellationToken;

#[derive(Debug, Clone, Serialize)]
pub struct SystemStats {
    pub cpu_usage: f32,
    pub memory_total: u64,
    pub memory_used: u64,
    pub memory_usage_percent: f32,
    pub load_avg_1: f64,
    pub load_avg_5: f64,
    pub load_avg_15: f64,
}

#[allow(missing_debug_implementations)]
pub struct SystemStatsCollector;

impl SystemStatsCollector {
    pub fn start(tx: broadcast::Sender<SystemStats>, cancel: CancellationToken) {
        tokio::spawn(async move {
            let mut sys = System::new();

            loop {
                tokio::select! {
                    () = cancel.cancelled() => break,
                    () = tokio::time::sleep(std::time::Duration::from_secs(2)) => {
                        sys.refresh_cpu_all();
                        sys.refresh_memory();
                        let load = System::load_average();
                        let total_memory = sys.total_memory();
                        let used_memory = sys.used_memory();
                        let stats = SystemStats {
                            cpu_usage: sys.global_cpu_usage(),
                            memory_total: total_memory,
                            memory_used: used_memory,
                            memory_usage_percent: if total_memory > 0 {
                                (used_memory as f32 / total_memory as f32) * 100.0
                            } else {
                                0.0
                            },
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

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::sync::broadcast;

    #[tokio::test]
    async fn test_broadcast_receiver_gets_stats() {
        let (tx, mut rx) = broadcast::channel(16);
        let cancel = CancellationToken::new();
        SystemStatsCollector::start(tx, cancel.clone());

        let stats = tokio::time::timeout(std::time::Duration::from_secs(5), rx.recv()).await;

        cancel.cancel();

        assert!(stats.is_ok());
        let stats = stats.unwrap().unwrap();
        assert!(stats.memory_total > 0);
    }
}
