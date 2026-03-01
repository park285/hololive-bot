use std::{
    collections::{HashMap, VecDeque},
    io,
    net::{IpAddr, Ipv4Addr},
    sync::{Arc, Mutex},
    time::Duration,
};

use chrono::{DateTime, TimeZone, Utc};
use reqwest::{Method, StatusCode};
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventLinkStatus, MajorEventStatus, MajorEventType},
};

use super::{
    HostResolver, HttpProbeResponse, LinkCheckRepository, LinkCheckResult, LinkChecker,
    LinkCheckerConfig, LinkHttpClient, should_fallback_to_get,
};

#[derive(Clone, Debug)]
enum FakeHttpResponse {
    Status {
        code: StatusCode,
        final_url: String,
        redirect_location: Option<String>,
    },
    Error(String),
}

type FakeHttpResultMap = Arc<Mutex<HashMap<String, VecDeque<FakeHttpResponse>>>>;

#[derive(Clone, Default)]
struct FakeHttpClient {
    results: FakeHttpResultMap,
    calls: Arc<Mutex<Vec<String>>>,
}

impl FakeHttpClient {
    fn push_status(&self, method: Method, url: &str, code: u16) {
        self.push_status_with_final_url(method, url, url, code);
    }

    fn push_status_with_final_url(&self, method: Method, url: &str, final_url: &str, code: u16) {
        let mut guard = self.results.lock().expect("results mutex poisoned");
        guard
            .entry(format!("{} {}", method.as_str(), url))
            .or_default()
            .push_back(FakeHttpResponse::Status {
                code: StatusCode::from_u16(code).expect("valid status"),
                final_url: final_url.to_string(),
                redirect_location: None,
            });
    }

    fn push_redirect(&self, method: Method, url: &str, location: &str, code: u16) {
        let mut guard = self.results.lock().expect("results mutex poisoned");
        guard
            .entry(format!("{} {}", method.as_str(), url))
            .or_default()
            .push_back(FakeHttpResponse::Status {
                code: StatusCode::from_u16(code).expect("valid status"),
                final_url: url.to_string(),
                redirect_location: Some(location.to_string()),
            });
    }

    fn push_error(&self, method: Method, url: &str, message: &str) {
        let mut guard = self.results.lock().expect("results mutex poisoned");
        guard
            .entry(format!("{} {}", method.as_str(), url))
            .or_default()
            .push_back(FakeHttpResponse::Error(message.to_string()));
    }

    fn calls(&self) -> Vec<String> {
        self.calls.lock().expect("calls mutex poisoned").clone()
    }
}

impl LinkHttpClient for FakeHttpClient {
    fn probe(
        &self,
        method: Method,
        url: &str,
        _timeout: Duration,
    ) -> super::BoxFuture<'_, Result<HttpProbeResponse, String>> {
        let key = format!("{} {}", method.as_str(), url);
        let calls = Arc::clone(&self.calls);
        let results = Arc::clone(&self.results);
        let default_response = FakeHttpResponse::Status {
            code: StatusCode::NOT_FOUND,
            final_url: url.to_string(),
            redirect_location: None,
        };

        Box::pin(async move {
            calls
                .lock()
                .expect("calls mutex poisoned")
                .push(key.clone());

            let next = {
                let mut guard = results.lock().expect("results mutex poisoned");
                guard.get_mut(&key).and_then(VecDeque::pop_front)
            };

            match next.unwrap_or(default_response) {
                FakeHttpResponse::Status {
                    code,
                    final_url,
                    redirect_location,
                } => Ok(HttpProbeResponse {
                    status: code,
                    response_url: final_url,
                    redirect_location,
                }),
                FakeHttpResponse::Error(message) => Err(message),
            }
        })
    }
}

#[derive(Clone, Debug)]
enum FakeResolveResult {
    Ips(Vec<IpAddr>),
    Error(String),
}

type FakeResolveResultMap = Arc<Mutex<HashMap<String, VecDeque<FakeResolveResult>>>>;

#[derive(Clone, Default)]
struct FakeHostResolver {
    results: FakeResolveResultMap,
}

impl FakeHostResolver {
    fn set_ips(&self, host: &str, ips: Vec<IpAddr>) {
        self.results
            .lock()
            .expect("resolver mutex poisoned")
            .insert(
                host.to_string(),
                VecDeque::from([FakeResolveResult::Ips(ips)]),
            );
    }

    fn set_ips_sequence(&self, host: &str, batches: Vec<Vec<IpAddr>>) {
        let queue: VecDeque<FakeResolveResult> = batches
            .into_iter()
            .map(FakeResolveResult::Ips)
            .collect::<VecDeque<_>>();

        self.results
            .lock()
            .expect("resolver mutex poisoned")
            .insert(host.to_string(), queue);
    }

    fn set_error(&self, host: &str, message: &str) {
        self.results
            .lock()
            .expect("resolver mutex poisoned")
            .insert(
                host.to_string(),
                VecDeque::from([FakeResolveResult::Error(message.to_string())]),
            );
    }
}

impl HostResolver for FakeHostResolver {
    fn resolve(&self, host: &str, _port: u16) -> super::BoxFuture<'_, Result<Vec<IpAddr>, String>> {
        let outcome = {
            let mut guard = self.results.lock().expect("resolver mutex poisoned");
            match guard.get_mut(host) {
                Some(queue) if queue.len() > 1 => queue.pop_front().expect("queue has entries"),
                Some(queue) => queue
                    .front()
                    .cloned()
                    .expect("queue has at least one entry"),
                None => FakeResolveResult::Error(format!("unexpected host: {host}")),
            }
        };

        Box::pin(async move {
            match outcome {
                FakeResolveResult::Ips(ips) => Ok(ips),
                FakeResolveResult::Error(err) => Err(err),
            }
        })
    }
}

#[derive(Clone, Default)]
struct FakeRepository {
    state: Arc<Mutex<FakeRepositoryState>>,
}

#[derive(Default)]
struct FakeRepositoryState {
    events: Vec<MajorEvent>,
    updates: HashMap<i32, MajorEventLinkStatus>,
    captured_stale_before: Option<DateTime<Utc>>,
    captured_limit: Option<i64>,
}

#[derive(Debug)]
struct FakeRepositorySnapshot {
    updates: HashMap<i32, MajorEventLinkStatus>,
    captured_stale_before: Option<DateTime<Utc>>,
    captured_limit: Option<i64>,
}

impl FakeRepository {
    fn new(events: Vec<MajorEvent>) -> Self {
        Self {
            state: Arc::new(Mutex::new(FakeRepositoryState {
                events,
                ..FakeRepositoryState::default()
            })),
        }
    }

    fn snapshot(&self) -> FakeRepositorySnapshot {
        let state = self.state.lock().expect("repository mutex poisoned");
        FakeRepositorySnapshot {
            updates: state.updates.clone(),
            captured_stale_before: state.captured_stale_before,
            captured_limit: state.captured_limit,
        }
    }
}

impl LinkCheckRepository for FakeRepository {
    fn list_events_needing_link_check(
        &self,
        stale_before: DateTime<Utc>,
        limit: i64,
    ) -> super::BoxFuture<'_, Result<Vec<MajorEvent>, ScraperError>> {
        let state = Arc::clone(&self.state);

        Box::pin(async move {
            let mut guard = state.lock().expect("repository mutex poisoned");
            guard.captured_stale_before = Some(stale_before);
            guard.captured_limit = Some(limit);
            Ok(guard.events.clone())
        })
    }

    fn update_event_link_status(
        &self,
        event_id: i32,
        status: MajorEventLinkStatus,
        _checked_at: DateTime<Utc>,
    ) -> super::BoxFuture<'_, Result<(), ScraperError>> {
        let state = Arc::clone(&self.state);

        Box::pin(async move {
            state
                .lock()
                .expect("repository mutex poisoned")
                .updates
                .insert(event_id, status);
            Ok(())
        })
    }
}

#[derive(Clone, Default)]
struct SharedWriter(Arc<Mutex<Vec<u8>>>);

impl SharedWriter {
    fn content(&self) -> String {
        let bytes = self.0.lock().expect("writer mutex poisoned").clone();
        String::from_utf8_lossy(&bytes).to_string()
    }
}

struct SharedWriterGuard(Arc<Mutex<Vec<u8>>>);

impl io::Write for SharedWriterGuard {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.0
            .lock()
            .expect("writer mutex poisoned")
            .extend_from_slice(buf);
        Ok(buf.len())
    }

    fn flush(&mut self) -> io::Result<()> {
        Ok(())
    }
}

impl<'a> tracing_subscriber::fmt::MakeWriter<'a> for SharedWriter {
    type Writer = SharedWriterGuard;

    fn make_writer(&'a self) -> Self::Writer {
        SharedWriterGuard(Arc::clone(&self.0))
    }
}

fn fixed_now() -> DateTime<Utc> {
    Utc.with_ymd_and_hms(2026, 2, 19, 8, 0, 0)
        .single()
        .expect("valid datetime")
}

fn test_event(id: i32, link: &str) -> MajorEvent {
    let now = fixed_now();

    MajorEvent {
        id,
        external_id: format!("event-{id}"),
        event_type: MajorEventType::Event,
        title: format!("event-{id}"),
        link: link.to_string(),
        description: None,
        members: Vec::new(),
        pub_date: None,
        event_start_date: None,
        event_end_date: None,
        event_dates: Vec::new(),
        status: MajorEventStatus::Active,
        link_status: MajorEventLinkStatus::Unchecked,
        link_checked_at: None,
        notified_at: None,
        notified_week: None,
        notified_month: None,
        created_at: now,
        updated_at: now,
    }
}

fn new_test_checker(http_client: FakeHttpClient, host_resolver: FakeHostResolver) -> LinkChecker {
    let mut checker = LinkChecker::new(
        reqwest::Client::new(),
        LinkCheckerConfig {
            timeout: Duration::from_secs(3),
            stale_hours: 72,
            batch_size: 50,
            max_concurrency: 4,
            max_redirect_hops: 5,
        },
    );

    checker.http_client = Arc::new(http_client);
    checker.host_resolver = Arc::new(host_resolver);
    checker.now = Arc::new(fixed_now);
    checker
}

#[tokio::test]
async fn check_link_head_success() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news/1";
    http_client.push_status(Method::HEAD, url, 200);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Ok);
    assert!(err.is_none(), "expected no error, got {err:?}");
    assert_eq!(http_client.calls(), vec![format!("HEAD {url}")]);
}

#[tokio::test]
async fn check_link_head_405_fallback_get_success() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news/2";
    http_client.push_status(Method::HEAD, url, 405);
    http_client.push_status(Method::GET, url, 200);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Ok);
    assert!(err.is_none(), "expected no error, got {err:?}");
    assert_eq!(
        http_client.calls(),
        vec![format!("HEAD {url}"), format!("GET {url}")]
    );
}

#[tokio::test]
async fn check_link_head_error_fallback_get_success() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news/3";
    http_client.push_error(Method::HEAD, url, "connection reset");
    http_client.push_status(Method::GET, url, 200);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Ok);
    assert!(err.is_none(), "expected no error, got {err:?}");
    assert_eq!(
        http_client.calls(),
        vec![format!("HEAD {url}"), format!("GET {url}")]
    );
}

#[tokio::test]
async fn check_link_head_404_fails_without_fallback() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news/404";
    http_client.push_status(Method::HEAD, url, 404);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Failed);
    assert!(
        err.unwrap_or_default().contains("head status code: 404"),
        "expected 404 error message"
    );
    assert_eq!(http_client.calls(), vec![format!("HEAD {url}")]);
}

#[tokio::test]
async fn check_link_redirect_to_blocked_hostname_is_blocked() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news";
    http_client.push_status_with_final_url(
        Method::HEAD,
        url,
        "https://localhost.local/blocked",
        302,
    );
    http_client.push_status(Method::GET, url, 200);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default().contains("blocked hostname"),
        "expected blocked hostname on redirect host"
    );
    assert_eq!(http_client.calls(), vec![format!("HEAD {url}")]);
}

#[tokio::test]
async fn check_link_redirect_to_private_resolved_ip_is_blocked() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );
    host_resolver.set_ips(
        "evil.example",
        vec![IpAddr::V4(Ipv4Addr::new(10, 10, 10, 10))],
    );

    let url = "https://example.com/news";
    http_client.push_status_with_final_url(Method::HEAD, url, "https://evil.example/news", 301);

    let checker = new_test_checker(http_client, host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default().contains("blocked resolved ip"),
        "expected blocked resolved ip on redirect host"
    );
}

#[tokio::test]
async fn check_link_redirect_to_unsupported_scheme_is_blocked() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news";
    http_client.push_status_with_final_url(Method::HEAD, url, "ftp://example.com/file", 302);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default()
            .contains("unsupported redirect scheme"),
        "expected unsupported redirect scheme to be blocked"
    );
    assert_eq!(http_client.calls(), vec![format!("HEAD {url}")]);
}

#[tokio::test]
async fn check_link_redirect_chain_revalidates_every_hop() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );
    host_resolver.set_ips(
        "safe.example",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 35))],
    );
    host_resolver.set_ips(
        "blocked.example",
        vec![IpAddr::V4(Ipv4Addr::new(10, 0, 0, 8))],
    );

    let start = "https://example.com/news";
    let step_1 = "https://safe.example/redirect";
    http_client.push_redirect(Method::HEAD, start, step_1, 302);
    http_client.push_redirect(
        Method::HEAD,
        step_1,
        "https://blocked.example/internal",
        302,
    );

    let checker = new_test_checker(http_client.clone(), host_resolver);
    let (status, err) = checker.check_link(start).await;

    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default().contains("blocked resolved ip"),
        "expected blocked resolved ip on redirect chain"
    );
    assert_eq!(
        http_client.calls(),
        vec![format!("HEAD {start}"), format!("HEAD {step_1}")]
    );
}

#[tokio::test]
async fn check_link_redirect_loop_returns_failed() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let url = "https://example.com/news";
    http_client.push_redirect(Method::HEAD, url, url, 302);

    let checker = new_test_checker(http_client.clone(), host_resolver);
    let (status, err) = checker.check_link(url).await;
    let err_msg = err.clone().unwrap_or_default();

    assert_eq!(status, MajorEventLinkStatus::Failed);
    assert!(
        err_msg.contains("redirect"),
        "expected redirect-related failure, got: {err_msg}"
    );
    assert_eq!(http_client.calls(), vec![format!("HEAD {url}")]);
}

#[tokio::test]
async fn check_link_get_fallback_revalidates_host_and_blocks_dns_rebinding() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips_sequence(
        "example.com",
        vec![
            vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
            vec![IpAddr::V4(Ipv4Addr::new(10, 0, 0, 5))],
        ],
    );

    let url = "https://example.com/news";
    http_client.push_status(Method::HEAD, url, 405);
    http_client.push_status(Method::GET, url, 200);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link(url).await;
    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default().contains("blocked resolved ip"),
        "expected dns rebinding revalidation to block private ip"
    );
    assert_eq!(
        http_client.calls(),
        vec![format!("HEAD {url}")],
        "GET should not run when fallback revalidation fails"
    );
}

#[tokio::test]
async fn check_link_blocks_localhost() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link("http://localhost/internal").await;
    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default().contains("blocked hostname"),
        "expected blocked hostname error"
    );
    assert!(http_client.calls().is_empty());
}

#[tokio::test]
async fn check_link_blocks_private_resolved_ip() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "private.example",
        vec![IpAddr::V4(Ipv4Addr::new(10, 0, 0, 5))],
    );

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let (status, err) = checker.check_link("https://private.example/news").await;
    assert_eq!(status, MajorEventLinkStatus::Blocked);
    assert!(
        err.unwrap_or_default().contains("blocked resolved ip"),
        "expected blocked resolved ip error"
    );
    assert!(http_client.calls().is_empty());
}

#[test]
fn is_success_status_accepts_redirect_3xx() {
    use super::is_success_status;
    // redirect 비활성화 시 3xx 직접 수신 → OK 판정 확인
    assert!(is_success_status(StatusCode::MOVED_PERMANENTLY));
    assert!(is_success_status(StatusCode::FOUND));
    assert!(is_success_status(StatusCode::TEMPORARY_REDIRECT));
    assert!(is_success_status(StatusCode::PERMANENT_REDIRECT));
}

#[test]
fn should_fallback_to_get_matches_go_behavior() {
    let tests = [
        ("request error", 0, Some("boom"), true),
        ("405", 405, None, true),
        ("403", 403, None, true),
        ("504", 504, None, true),
        ("404", 404, None, false),
        ("200", 200, None, false),
    ];

    for (name, status_code, err, expected) in tests {
        let actual = should_fallback_to_get(status_code, err);
        assert_eq!(actual, expected, "test case failed: {name}");
    }
}

#[tokio::test]
async fn check_stale_links_updates_statuses_and_captures_stale_window() {
    let repo = FakeRepository::new(vec![
        test_event(1, "https://example.com/news/ok"),
        test_event(2, "http://localhost/internal"),
    ]);

    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_ips(
        "example.com",
        vec![IpAddr::V4(Ipv4Addr::new(93, 184, 216, 34))],
    );

    let ok_url = "https://example.com/news/ok";
    http_client.push_status(Method::HEAD, ok_url, 200);

    let checker = new_test_checker(http_client.clone(), host_resolver);

    let result = checker
        .check_stale_links(&repo)
        .await
        .expect("check_stale_links should succeed");

    assert_eq!(
        result,
        LinkCheckResult {
            checked: 2,
            ok: 1,
            failed: 0,
            blocked: 1,
        }
    );

    let snapshot = repo.snapshot();
    let expected_stale_before = fixed_now() - chrono::Duration::hours(72);

    assert_eq!(snapshot.captured_stale_before, Some(expected_stale_before));
    assert_eq!(snapshot.captured_limit, Some(50));
    assert_eq!(snapshot.updates.get(&1), Some(&MajorEventLinkStatus::Ok));
    assert_eq!(
        snapshot.updates.get(&2),
        Some(&MajorEventLinkStatus::Blocked)
    );
}

#[tokio::test(flavor = "current_thread")]
async fn check_stale_links_no_events_logs_debug() {
    let repo = FakeRepository::new(Vec::new());
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    let checker = new_test_checker(http_client.clone(), host_resolver);

    let writer = SharedWriter::default();
    let subscriber = tracing_subscriber::fmt()
        .with_max_level(tracing::Level::DEBUG)
        .with_writer(writer.clone())
        .with_ansi(false)
        .without_time()
        .finish();
    let _guard = tracing::subscriber::set_default(subscriber);

    let result = checker
        .check_stale_links(&repo)
        .await
        .expect("check_stale_links should succeed");

    assert_eq!(result, LinkCheckResult::default());
    assert!(http_client.calls().is_empty());

    let logs = writer.content();
    assert!(
        logs.contains("major event link check skipped: no stale targets"),
        "expected debug log for no stale targets"
    );
}

#[tokio::test]
async fn check_link_resolver_error_returns_failed() {
    let http_client = FakeHttpClient::default();
    let host_resolver = FakeHostResolver::default();
    host_resolver.set_error("broken.example", "dns timeout");

    let checker = new_test_checker(http_client, host_resolver);

    let (status, err) = checker.check_link("https://broken.example/news").await;
    assert_eq!(status, MajorEventLinkStatus::Failed);
    assert!(
        err.unwrap_or_default()
            .contains("resolve host broken.example"),
        "expected resolver error"
    );
}
