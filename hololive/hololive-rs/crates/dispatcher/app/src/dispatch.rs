use std::{sync::Arc, time::Duration};

use anyhow::Result;
use shared_core::error::SharedError;
use shared_formatter::ResponseFormatter;
use shared_infra::iris::IrisClient;
use shared_notification::{QueueConsumer, ValkeyQueueConsumer};
use shared_template::Renderer;
use tokio_util::sync::CancellationToken;
use tracing::{debug, info, warn};

use crate::{
    grouping::group_queue_envelopes,
    render::render_group_message,
    state::{AppState, clear_last_error, set_last_error, update_valkey_connection},
};

pub(crate) struct DispatchRuntime {
    pub consumer: ValkeyQueueConsumer,
    pub formatter: ResponseFormatter,
    pub renderer: Renderer,
    pub iris_client: IrisClient,
    pub max_batch: usize,
    pub reconnect_backoff: Duration,
}

pub(crate) async fn run_dispatch_loop(
    runtime: DispatchRuntime,
    state: Arc<AppState>,
    shutdown_token: CancellationToken,
) -> Result<()> {
    loop {
        let drain_result = tokio::select! {
            () = shutdown_token.cancelled() => {
                info!("dispatcher loop shutdown requested");
                break;
            }
            result = run_dispatch_once(&runtime, state.as_ref()) => result,
        };

        match drain_result {
            Ok(()) => {}
            Err(error) => {
                if is_valkey_timeout_error(&error) {
                    update_valkey_connection(&state, true);
                    clear_last_error(&state);
                    debug!("dispatch queue poll timeout");
                    continue;
                }

                update_valkey_connection(&state, false);
                set_last_error(&state, format!("drain batch failed: {error}"));
                warn!(error = %error, "failed to consume dispatch queue");

                tokio::select! {
                    () = shutdown_token.cancelled() => {
                        info!("dispatcher loop shutdown during backoff");
                        break;
                    }
                    () = tokio::time::sleep(runtime.reconnect_backoff) => {}
                }
            }
        }
    }

    Ok(())
}

fn is_valkey_timeout_error(error: &SharedError) -> bool {
    matches!(error, SharedError::Valkey(message) if message.contains("Timeout Error: Request timed out."))
}

pub(crate) async fn run_dispatch_once(
    runtime: &DispatchRuntime,
    state: &AppState,
) -> Result<(), SharedError> {
    let envelopes = runtime.consumer.drain_batch(runtime.max_batch).await?;

    update_valkey_connection(state, true);
    clear_last_error(state);

    if envelopes.is_empty() {
        return Ok(());
    }

    let groups = group_queue_envelopes(envelopes);

    for group in groups {
        let message = render_group_message(&runtime.renderer, &runtime.formatter, &group);

        if let Err(error) = runtime
            .iris_client
            .send_reply(&group.room_id, &message)
            .await
        {
            warn!(
                error = %error,
                room_id = %group.room_id,
                notifications = group.notifications.len(),
                "dispatch send failed; releasing claim keys"
            );
            set_last_error(state, format!("send reply failed: {error}"));

            if let Err(release_error) = runtime.consumer.release_claim_keys(&group.claim_keys).await
            {
                warn!(
                    error = %release_error,
                    room_id = %group.room_id,
                    "release claim keys failed"
                );
            }
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use std::{
        collections::{HashMap, VecDeque},
        sync::{
            Arc, Mutex, RwLock,
            atomic::{AtomicBool, AtomicUsize, Ordering},
        },
        time::Duration,
    };

    use async_trait::async_trait;
    use axum::{Json, Router, extract::State, http::StatusCode, routing::post};
    use chrono::{TimeZone, Utc};
    use secrecy::SecretString;
    use serde_json::json;
    use shared_core::{
        error::SharedError,
        model::{AlarmNotification, AlarmQueueEnvelope, Stream, StreamStatus},
    };
    use shared_infra::{
        iris::IrisClient,
        valkey::{MockValkeyClient, ValkeyClient},
    };
    use shared_notification::ValkeyQueueConsumer;
    use shared_template::Renderer;
    use tokio::{net::TcpListener, task::JoinHandle, time::Instant};
    use tokio_util::sync::CancellationToken;

    use super::{DispatchRuntime, run_dispatch_loop, run_dispatch_once};
    use crate::{
        render::{ALARM_LIVE_STARTED_TEMPLATE_KEY, ALARM_NOTIFICATION_TEMPLATE_KEY},
        state::{AppState, snapshot_last_error},
    };

    #[tokio::test]
    async fn dispatch_loop_sends_reply_on_successful_iris_response() {
        let queue_key = "alarm:dispatch:queue:test:dispatch-success";
        let claim_key = "notified:claim:room-success:stream-success:1740811200:live";

        let valkey_client = Arc::new(MockValkeyClient::new());
        enqueue_notification(
            valkey_client.as_ref(),
            queue_key,
            claim_key,
            "room-success",
            "stream-success",
            5,
        )
        .await;

        let (base_url, request_count, server_shutdown, server_handle) =
            spawn_iris_server(StatusCode::OK).await;
        let consumer_client: Arc<dyn ValkeyClient> = valkey_client;

        let runtime = DispatchRuntime {
            consumer: ValkeyQueueConsumer::new(consumer_client).with_queue_key(queue_key),
            formatter: shared_formatter::ResponseFormatter::new(""),
            renderer: build_test_renderer(),
            iris_client: IrisClient::new(&base_url, SecretString::from("token"))
                .expect("build iris test client"),
            max_batch: 10,
            reconnect_backoff: Duration::from_millis(10),
        };

        let state = Arc::new(AppState {
            version: "test",
            started_at: Utc::now(),
            valkey_connected: AtomicBool::new(true),
            last_error: RwLock::new(None),
        });

        run_dispatch_once(&runtime, state.as_ref())
            .await
            .expect("dispatch once should succeed");
        server_shutdown.cancel();

        server_handle
            .await
            .expect("join iris server")
            .expect("iris server should stop cleanly");

        assert_eq!(request_count.load(Ordering::Relaxed), 1);
        assert_eq!(snapshot_last_error(state.as_ref()), None);
    }

    #[tokio::test]
    async fn dispatch_loop_releases_claim_keys_on_iris_failure() {
        let queue_key = "alarm:dispatch:queue:test:dispatch-failure";
        let claim_key = "notified:claim:room-failure:stream-failure:1740811200:live";

        let valkey_client = Arc::new(MockValkeyClient::new());
        valkey_client
            .set(claim_key, "claimed", None)
            .await
            .expect("seed claim key");
        enqueue_notification(
            valkey_client.as_ref(),
            queue_key,
            claim_key,
            "room-failure",
            "stream-failure",
            0,
        )
        .await;

        let (base_url, request_count, server_shutdown, server_handle) =
            spawn_iris_server(StatusCode::INTERNAL_SERVER_ERROR).await;
        let consumer_client: Arc<dyn ValkeyClient> = valkey_client.clone();

        let runtime = DispatchRuntime {
            consumer: ValkeyQueueConsumer::new(consumer_client).with_queue_key(queue_key),
            formatter: shared_formatter::ResponseFormatter::new(""),
            renderer: build_test_renderer(),
            iris_client: IrisClient::new(&base_url, SecretString::from("token"))
                .expect("build iris test client"),
            max_batch: 10,
            reconnect_backoff: Duration::from_millis(10),
        };

        let state = Arc::new(AppState {
            version: "test",
            started_at: Utc::now(),
            valkey_connected: AtomicBool::new(true),
            last_error: RwLock::new(None),
        });

        run_dispatch_once(&runtime, state.as_ref())
            .await
            .expect("dispatch once should complete even when send fails");
        server_shutdown.cancel();

        server_handle
            .await
            .expect("join iris server")
            .expect("iris server should stop cleanly");

        assert_eq!(request_count.load(Ordering::Relaxed), 1);
        assert!(
            valkey_client
                .get(claim_key)
                .await
                .expect("read claim key")
                .is_none(),
            "claim key should be deleted"
        );
        assert!(
            snapshot_last_error(state.as_ref())
                .is_some_and(|error| error.contains("send reply failed")),
            "last_error should capture send failure"
        );
    }

    #[tokio::test]
    async fn dispatch_loop_marks_degraded_and_recovers_after_valkey_error() {
        let queue_key = "alarm:dispatch:queue:test:degraded-recover";
        let claim_key = "notified:claim:room-recover:stream-recover:1740811200:live";
        let envelope = AlarmQueueEnvelope {
            notification: AlarmNotification::new(
                "room-recover".to_owned(),
                None,
                Some(make_stream("stream-recover")),
                5,
                Vec::new(),
                String::new(),
            ),
            claim_keys: vec![claim_key.to_owned()],
            enqueued_at: Utc::now().to_rfc3339(),
            version: 1,
        };
        let payload = serde_json::to_string(&envelope).expect("serialize recovery envelope");

        let scripted_client = Arc::new(ScriptedValkeyClient::new(vec![
            BrpopStep::Error("simulated valkey outage".to_owned()),
            BrpopStep::Payload(payload),
            BrpopStep::Empty,
        ]));
        let consumer_client: Arc<dyn ValkeyClient> = scripted_client.clone();

        let (base_url, request_count, server_shutdown, server_handle) =
            spawn_iris_server(StatusCode::OK).await;

        let runtime = DispatchRuntime {
            consumer: ValkeyQueueConsumer::new(consumer_client).with_queue_key(queue_key),
            formatter: shared_formatter::ResponseFormatter::new(""),
            renderer: build_test_renderer(),
            iris_client: IrisClient::new(&base_url, SecretString::from("token"))
                .expect("build iris test client"),
            max_batch: 1,
            reconnect_backoff: Duration::from_millis(10),
        };

        let state = Arc::new(AppState {
            version: "test",
            started_at: Utc::now(),
            valkey_connected: AtomicBool::new(true),
            last_error: RwLock::new(None),
        });

        let shutdown_token = CancellationToken::new();
        let loop_handle = tokio::spawn(run_dispatch_loop(
            runtime,
            Arc::clone(&state),
            shutdown_token.clone(),
        ));

        wait_until(
            Duration::from_secs(1),
            || !state.valkey_connected.load(Ordering::Relaxed),
            "valkey should become degraded after drain error",
        )
        .await;
        assert!(
            snapshot_last_error(state.as_ref())
                .is_some_and(|error| error.contains("drain batch failed")),
            "last_error should record valkey drain failure"
        );

        wait_until(
            Duration::from_secs(1),
            || request_count.load(Ordering::Relaxed) >= 1,
            "queue should be dispatched after recovery",
        )
        .await;
        wait_until(
            Duration::from_secs(1),
            || state.valkey_connected.load(Ordering::Relaxed),
            "valkey status should recover to connected",
        )
        .await;
        wait_until(
            Duration::from_secs(1),
            || snapshot_last_error(state.as_ref()).is_none(),
            "last_error should be cleared after successful dispatch",
        )
        .await;

        shutdown_token.cancel();
        server_shutdown.cancel();

        let dispatch_result = loop_handle.await.expect("join dispatch loop");
        assert!(dispatch_result.is_ok(), "dispatch loop should stop cleanly");

        server_handle
            .await
            .expect("join iris server")
            .expect("iris server should stop cleanly");

        assert_eq!(request_count.load(Ordering::Relaxed), 1);
        assert!(state.valkey_connected.load(Ordering::Relaxed));
        assert_eq!(snapshot_last_error(state.as_ref()), None);
    }

    fn build_test_renderer() -> Renderer {
        let renderer = Renderer::new();
        renderer
            .insert_template_body(
                ALARM_NOTIFICATION_TEMPLATE_KEY,
                "⏰ {{ .ChannelName }} 방송 예정\n📺 {{ .Title }}\n🔗 {{ .URL }}",
            )
            .expect("insert notification template");
        renderer
            .insert_template_body(
                ALARM_LIVE_STARTED_TEMPLATE_KEY,
                "🔴 {{ .ChannelName }} 방송 시작됨\n📺 {{ .Title }}\n🔗 {{ .URL }}",
            )
            .expect("insert live template");
        renderer
    }

    async fn enqueue_notification(
        valkey_client: &MockValkeyClient,
        queue_key: &str,
        claim_key: &str,
        room_id: &str,
        stream_id: &str,
        minutes_until: i32,
    ) {
        let envelope = AlarmQueueEnvelope {
            notification: AlarmNotification::new(
                room_id.to_owned(),
                None,
                Some(make_stream(stream_id)),
                minutes_until,
                Vec::new(),
                String::new(),
            ),
            claim_keys: vec![claim_key.to_owned()],
            enqueued_at: Utc::now().to_rfc3339(),
            version: 1,
        };

        let payload = serde_json::to_string(&envelope).expect("serialize envelope");
        valkey_client
            .lpush(queue_key, &payload)
            .await
            .expect("push envelope to queue");
    }

    fn make_stream(stream_id: &str) -> Stream {
        Stream {
            id: stream_id.to_owned(),
            title: format!("title-{stream_id}"),
            channel_id: "channel-id".to_owned(),
            channel_name: "channel-name".to_owned(),
            status: StreamStatus::Upcoming,
            start_scheduled: Utc.with_ymd_and_hms(2026, 3, 1, 12, 0, 0).single(),
            start_actual: None,
            duration: None,
            thumbnail: None,
            link: None,
            topic_id: None,
            channel: None,
            viewer_count: None,
            chzzk_channel_id: String::new(),
            chzzk_live_id: 0,
            chzzk_live_url: String::new(),
            is_integrated: false,
            is_chzzk_only: false,
            twitch_user_id: String::new(),
            twitch_user_login: String::new(),
            twitch_stream_id: String::new(),
            twitch_live_url: String::new(),
            is_twitch_only: false,
        }
    }

    enum BrpopStep {
        Error(String),
        Payload(String),
        Empty,
    }

    struct ScriptedValkeyClient {
        inner: MockValkeyClient,
        brpop_steps: Mutex<VecDeque<BrpopStep>>,
    }

    impl ScriptedValkeyClient {
        fn new(steps: Vec<BrpopStep>) -> Self {
            Self {
                inner: MockValkeyClient::new(),
                brpop_steps: Mutex::new(VecDeque::from(steps)),
            }
        }
    }

    #[async_trait]
    impl ValkeyClient for ScriptedValkeyClient {
        async fn get(&self, key: &str) -> Result<Option<String>, SharedError> {
            self.inner.get(key).await
        }

        async fn set(
            &self,
            key: &str,
            value: &str,
            ttl: Option<Duration>,
        ) -> Result<(), SharedError> {
            self.inner.set(key, value, ttl).await
        }

        async fn set_nx(&self, key: &str, value: &str, ttl: Duration) -> Result<bool, SharedError> {
            self.inner.set_nx(key, value, ttl).await
        }

        async fn del(&self, keys: &[&str]) -> Result<(), SharedError> {
            self.inner.del(keys).await
        }

        async fn hget(&self, key: &str, field: &str) -> Result<Option<String>, SharedError> {
            self.inner.hget(key, field).await
        }

        async fn hset(&self, key: &str, field: &str, value: &str) -> Result<(), SharedError> {
            self.inner.hset(key, field, value).await
        }

        async fn hget_all(&self, key: &str) -> Result<HashMap<String, String>, SharedError> {
            self.inner.hget_all(key).await
        }

        async fn hmset(&self, key: &str, fields: &[(String, String)]) -> Result<(), SharedError> {
            self.inner.hmset(key, fields).await
        }

        async fn smembers(&self, key: &str) -> Result<Vec<String>, SharedError> {
            self.inner.smembers(key).await
        }

        async fn smembers_multi(&self, keys: &[String]) -> Result<Vec<Vec<String>>, SharedError> {
            self.inner.smembers_multi(keys).await
        }

        async fn expire(&self, key: &str, ttl: Duration) -> Result<(), SharedError> {
            self.inner.expire(key, ttl).await
        }

        async fn lpush(&self, key: &str, value: &str) -> Result<(), SharedError> {
            self.inner.lpush(key, value).await
        }

        async fn ping(&self) -> Result<(), SharedError> {
            self.inner.ping().await
        }

        async fn brpop(&self, _key: &str, _timeout: f64) -> Result<Option<String>, SharedError> {
            let step = self
                .brpop_steps
                .lock()
                .expect("lock scripted brpop steps")
                .pop_front()
                .unwrap_or(BrpopStep::Empty);

            match step {
                BrpopStep::Error(message) => Err(SharedError::Valkey(message)),
                BrpopStep::Payload(payload) => Ok(Some(payload)),
                BrpopStep::Empty => {
                    tokio::time::sleep(Duration::from_millis(5)).await;
                    Ok(None)
                }
            }
        }

        async fn llen(&self, key: &str) -> Result<i64, SharedError> {
            self.inner.llen(key).await
        }
    }

    async fn spawn_iris_server(
        status: StatusCode,
    ) -> (
        String,
        Arc<AtomicUsize>,
        CancellationToken,
        JoinHandle<anyhow::Result<()>>,
    ) {
        #[derive(Clone)]
        struct IrisState {
            status: StatusCode,
            requests: Arc<AtomicUsize>,
        }

        async fn reply(
            State(state): State<IrisState>,
            Json(_payload): Json<serde_json::Value>,
        ) -> (StatusCode, Json<serde_json::Value>) {
            state.requests.fetch_add(1, Ordering::Relaxed);
            (
                state.status,
                Json(json!({
                    "status": state.status.as_u16()
                })),
            )
        }

        let listener = TcpListener::bind("127.0.0.1:0")
            .await
            .expect("bind iris mock server");
        let address = listener.local_addr().expect("read iris mock address");
        let requests = Arc::new(AtomicUsize::new(0));
        let shutdown_token = CancellationToken::new();

        let app = Router::new()
            .route("/api/v1/reply", post(reply))
            .with_state(IrisState {
                status,
                requests: Arc::clone(&requests),
            });

        let server_shutdown = shutdown_token.clone();
        let handle = tokio::spawn(async move {
            axum::serve(listener, app)
                .with_graceful_shutdown(server_shutdown.cancelled_owned())
                .await
                .map_err(|error| anyhow::anyhow!("serve iris mock server: {error}"))
        });

        tokio::time::sleep(Duration::from_millis(20)).await;
        (
            format!("http://{address}"),
            requests,
            shutdown_token,
            handle,
        )
    }

    /// P1-3.4: 배치 처리 latency 측정 + error rate 검증
    ///
    /// 50개 notification 배치를 반복 처리하여 p95 latency < 1s, error rate < 0.1% 확인.
    #[tokio::test]
    async fn dispatch_batch_latency_p95_under_1s_and_low_error_rate() {
        const BATCH_SIZE: usize = 50;
        const ITERATIONS: usize = 20;

        let queue_key = "alarm:dispatch:queue:test:perf";
        let valkey_client = Arc::new(MockValkeyClient::new());

        let (base_url, request_count, server_shutdown, server_handle) =
            spawn_iris_server(StatusCode::OK).await;
        let consumer_client: Arc<dyn ValkeyClient> = valkey_client.clone();

        let runtime = DispatchRuntime {
            consumer: ValkeyQueueConsumer::new(consumer_client).with_queue_key(queue_key),
            formatter: shared_formatter::ResponseFormatter::new(""),
            renderer: build_test_renderer(),
            iris_client: IrisClient::new(&base_url, SecretString::from("token"))
                .expect("build iris test client"),
            max_batch: BATCH_SIZE,
            reconnect_backoff: Duration::from_millis(10),
        };

        let state = Arc::new(AppState {
            version: "test",
            started_at: Utc::now(),
            valkey_connected: AtomicBool::new(true),
            last_error: RwLock::new(None),
        });

        let mut latencies = Vec::with_capacity(ITERATIONS);
        let mut errors: usize = 0;
        let total_dispatches = ITERATIONS * BATCH_SIZE;

        for i in 0..ITERATIONS {
            // 매 반복마다 BATCH_SIZE개 notification enqueue
            for j in 0..BATCH_SIZE {
                let claim = format!("notified:claim:room-perf:stream-{i}-{j}:1740811200:live");
                enqueue_notification(
                    valkey_client.as_ref(),
                    queue_key,
                    &claim,
                    "room-perf",
                    &format!("stream-{i}-{j}"),
                    5,
                )
                .await;
            }

            let start = Instant::now();
            match run_dispatch_once(&runtime, state.as_ref()).await {
                Ok(()) => {}
                Err(_) => errors += 1,
            }
            latencies.push(start.elapsed());
        }

        server_shutdown.cancel();
        server_handle
            .await
            .expect("join iris server")
            .expect("iris server should stop cleanly");

        // p95 계산: 정렬 후 95번째 백분위
        latencies.sort();
        let p95_index = (latencies.len() as f64 * 0.95).ceil() as usize - 1;
        let p95 = latencies[p95_index];

        let error_rate = errors as f64 / ITERATIONS as f64;

        assert!(
            p95 < Duration::from_secs(1),
            "p95 latency {p95:?} exceeds 1s threshold"
        );
        assert!(
            error_rate < 0.001,
            "error rate {error_rate:.4} exceeds 0.1% threshold"
        );

        // 그룹화로 동일 room_id가 병합되므로 request_count >= ITERATIONS
        let total_requests = request_count.load(Ordering::Relaxed);
        assert!(
            total_requests >= ITERATIONS,
            "expected at least {ITERATIONS} iris requests, got {total_requests}"
        );
        assert_eq!(
            errors, 0,
            "expected 0 errors across {total_dispatches} dispatches"
        );
    }

    /// P1-3.5: Valkey 5분 단절(300 error steps) 장애 주입 → degraded 유지 → 복구 검증
    ///
    /// 시뮬레이션: 300회 연속 brpop 실패(10ms backoff × 300 ≈ 3s 실시간)로
    /// 5분 단절 시나리오를 압축 재현. 단절 중 degraded 유지, 복구 후 정상 dispatch 확인.
    #[tokio::test]
    async fn dispatch_loop_survives_prolonged_valkey_outage_and_recovers() {
        const OUTAGE_STEPS: usize = 300;

        let queue_key = "alarm:dispatch:queue:test:prolonged-outage";
        let claim_key = "notified:claim:room-outage:stream-outage:1740811200:live";
        let envelope = AlarmQueueEnvelope {
            notification: AlarmNotification::new(
                "room-outage".to_owned(),
                None,
                Some(make_stream("stream-outage")),
                5,
                Vec::new(),
                String::new(),
            ),
            claim_keys: vec![claim_key.to_owned()],
            enqueued_at: Utc::now().to_rfc3339(),
            version: 1,
        };
        let payload = serde_json::to_string(&envelope).expect("serialize outage envelope");

        // 300회 연속 에러 → 정상 payload → empty (종료 유도)
        let mut steps: Vec<BrpopStep> = Vec::with_capacity(OUTAGE_STEPS + 2);
        for _ in 0..OUTAGE_STEPS {
            steps.push(BrpopStep::Error("simulated 5-min valkey outage".to_owned()));
        }
        steps.push(BrpopStep::Payload(payload));
        steps.push(BrpopStep::Empty);

        let scripted_client = Arc::new(ScriptedValkeyClient::new(steps));
        let consumer_client: Arc<dyn ValkeyClient> = scripted_client.clone();

        let (base_url, request_count, server_shutdown, server_handle) =
            spawn_iris_server(StatusCode::OK).await;

        let runtime = DispatchRuntime {
            consumer: ValkeyQueueConsumer::new(consumer_client).with_queue_key(queue_key),
            formatter: shared_formatter::ResponseFormatter::new(""),
            renderer: build_test_renderer(),
            iris_client: IrisClient::new(&base_url, SecretString::from("token"))
                .expect("build iris test client"),
            max_batch: 1,
            reconnect_backoff: Duration::from_millis(10),
        };

        let state = Arc::new(AppState {
            version: "test",
            started_at: Utc::now(),
            valkey_connected: AtomicBool::new(true),
            last_error: RwLock::new(None),
        });

        let shutdown_token = CancellationToken::new();
        let loop_handle = tokio::spawn(run_dispatch_loop(
            runtime,
            Arc::clone(&state),
            shutdown_token.clone(),
        ));

        // 단절 단계: degraded 전환 확인
        wait_until(
            Duration::from_secs(2),
            || !state.valkey_connected.load(Ordering::Relaxed),
            "valkey should become degraded during prolonged outage",
        )
        .await;
        assert!(
            snapshot_last_error(state.as_ref()).is_some_and(|e| e.contains("drain batch failed")),
            "last_error should record drain failure during outage"
        );

        // 단절 중간 시점에서도 degraded 유지 확인 (backoff 소비 대기)
        tokio::time::sleep(Duration::from_millis(500)).await;
        assert!(
            !state.valkey_connected.load(Ordering::Relaxed),
            "valkey should remain degraded during sustained outage"
        );

        // 복구 단계: 모든 error step 소진 후 정상 dispatch
        wait_until(
            Duration::from_secs(10),
            || request_count.load(Ordering::Relaxed) >= 1,
            "dispatch should resume after outage recovery",
        )
        .await;
        wait_until(
            Duration::from_secs(2),
            || state.valkey_connected.load(Ordering::Relaxed),
            "valkey connection should recover after outage",
        )
        .await;
        wait_until(
            Duration::from_secs(2),
            || snapshot_last_error(state.as_ref()).is_none(),
            "last_error should be cleared after recovery",
        )
        .await;

        shutdown_token.cancel();
        server_shutdown.cancel();

        let dispatch_result = loop_handle.await.expect("join dispatch loop");
        assert!(dispatch_result.is_ok(), "dispatch loop should stop cleanly");

        server_handle
            .await
            .expect("join iris server")
            .expect("iris server should stop cleanly");

        // 최종 검증: 복구 후 정상 상태
        assert_eq!(request_count.load(Ordering::Relaxed), 1);
        assert!(state.valkey_connected.load(Ordering::Relaxed));
        assert_eq!(snapshot_last_error(state.as_ref()), None);
    }

    async fn wait_until<F>(timeout: Duration, condition: F, message: &str)
    where
        F: Fn() -> bool,
    {
        let started = Instant::now();
        while !condition() {
            assert!(started.elapsed() <= timeout, "{message}");
            tokio::time::sleep(Duration::from_millis(10)).await;
        }
    }
}
