use chrono::{DateTime, Utc};
use futures::{StreamExt, stream};
use hickory_resolver::{
    TokioResolver,
    config::{ResolverConfig, ResolverOpts},
    name_server::TokioConnectionProvider,
};
use scraper_core::{
    error::ScraperError,
    model::{MajorEvent, MajorEventLinkStatus},
};
use scraper_infra::repository::Repository;
use std::collections::HashSet;
use std::{future::Future, net::IpAddr, pin::Pin, sync::Arc, time::Duration};
use tracing::{debug, warn};
use url::Url;

type BoxFuture<'a, T> = Pin<Box<dyn Future<Output = T> + Send + 'a>>;

type NowFn = dyn Fn() -> DateTime<Utc> + Send + Sync;
const DEFAULT_MAX_REDIRECT_HOPS: usize = 5;

pub trait LinkCheckRepository: Send + Sync {
    fn list_events_needing_link_check(
        &self,
        stale_before: DateTime<Utc>,
        limit: i64,
    ) -> BoxFuture<'_, Result<Vec<MajorEvent>, ScraperError>>;

    fn update_event_link_status(
        &self,
        event_id: i32,
        status: MajorEventLinkStatus,
        checked_at: DateTime<Utc>,
    ) -> BoxFuture<'_, Result<(), ScraperError>>;
}

impl LinkCheckRepository for Repository {
    fn list_events_needing_link_check(
        &self,
        stale_before: DateTime<Utc>,
        limit: i64,
    ) -> BoxFuture<'_, Result<Vec<MajorEvent>, ScraperError>> {
        Box::pin(async move {
            Repository::list_events_needing_link_check(self, stale_before, limit).await
        })
    }

    fn update_event_link_status(
        &self,
        event_id: i32,
        status: MajorEventLinkStatus,
        checked_at: DateTime<Utc>,
    ) -> BoxFuture<'_, Result<(), ScraperError>> {
        Box::pin(async move {
            Repository::update_event_link_status(self, event_id, status, checked_at).await
        })
    }
}

trait LinkHttpClient: Send + Sync {
    fn probe(
        &self,
        method: reqwest::Method,
        url: &str,
        timeout: Duration,
    ) -> BoxFuture<'_, Result<HttpProbeResponse, String>>;
}

#[derive(Clone)]
struct ReqwestLinkHttpClient {
    client: reqwest::Client,
}

#[derive(Debug, Clone)]
struct HttpProbeResponse {
    status: reqwest::StatusCode,
    response_url: String,
    redirect_location: Option<String>,
}

#[derive(Debug)]
enum ProbeError {
    Blocked(String),
    Failed(String),
}

impl ReqwestLinkHttpClient {
    fn new(client: reqwest::Client) -> Self {
        Self { client }
    }
}

impl LinkHttpClient for ReqwestLinkHttpClient {
    fn probe(
        &self,
        method: reqwest::Method,
        url: &str,
        timeout: Duration,
    ) -> BoxFuture<'_, Result<HttpProbeResponse, String>> {
        let client = self.client.clone();
        let target_url = url.to_string();

        Box::pin(async move {
            let mut request = client.request(method.clone(), &target_url).timeout(timeout);

            if method == reqwest::Method::GET {
                request = request.header(reqwest::header::RANGE, "bytes=0-0");
            }

            let response = request.send().await.map_err(|err| err.to_string())?;
            let status = response.status();
            let response_url = response.url().to_string();
            let redirect_location = response
                .headers()
                .get(reqwest::header::LOCATION)
                .and_then(|value| value.to_str().ok())
                .map(ToString::to_string);

            Ok(HttpProbeResponse {
                status,
                response_url,
                redirect_location,
            })
        })
    }
}

trait HostResolver: Send + Sync {
    fn resolve(&self, host: &str, _port: u16) -> BoxFuture<'_, Result<Vec<IpAddr>, String>>;
}

#[derive(Clone)]
struct TokioHostResolver {
    resolver: TokioResolver,
}

impl Default for TokioHostResolver {
    fn default() -> Self {
        let resolver = TokioResolver::builder_with_config(
            ResolverConfig::default(),
            TokioConnectionProvider::default(),
        )
        .with_options(ResolverOpts::default())
        .build();
        Self { resolver }
    }
}

impl HostResolver for TokioHostResolver {
    fn resolve(&self, host: &str, _port: u16) -> BoxFuture<'_, Result<Vec<IpAddr>, String>> {
        let target_host = host.to_string();
        let resolver = self.resolver.clone();

        Box::pin(async move {
            let result = resolver
                .lookup_ip(target_host.as_str())
                .await
                .map_err(|err| err.to_string())?;

            let mut ips: Vec<IpAddr> = Vec::new();
            for ip in result.iter() {
                ips.push(ip);
            }

            Ok(ips)
        })
    }
}

#[derive(Debug, Clone)]
pub struct LinkCheckerConfig {
    pub timeout: Duration,
    pub stale_hours: i64,
    pub batch_size: usize,
    pub max_concurrency: usize,
    pub max_redirect_hops: usize,
}

impl Default for LinkCheckerConfig {
    fn default() -> Self {
        Self {
            timeout: Duration::from_secs(8),
            stale_hours: 72,
            batch_size: 200,
            max_concurrency: 16,
            max_redirect_hops: DEFAULT_MAX_REDIRECT_HOPS,
        }
    }
}

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct LinkCheckResult {
    pub checked: usize,
    pub ok: usize,
    pub failed: usize,
    pub blocked: usize,
}

#[derive(Clone)]
pub struct LinkChecker {
    http_client: Arc<dyn LinkHttpClient>,
    host_resolver: Arc<dyn HostResolver>,
    config: LinkCheckerConfig,
    now: Arc<NowFn>,
}

impl LinkChecker {
    pub fn new(client: reqwest::Client, config: LinkCheckerConfig) -> Self {
        Self {
            http_client: Arc::new(ReqwestLinkHttpClient::new(client)),
            host_resolver: Arc::new(TokioHostResolver::default()),
            config,
            now: Arc::new(Utc::now),
        }
    }

    pub async fn check_stale_links<R>(
        &self,
        repository: &R,
    ) -> Result<LinkCheckResult, ScraperError>
    where
        R: LinkCheckRepository + ?Sized,
    {
        let stale_before = (self.now)() - chrono::Duration::hours(self.config.stale_hours);
        let events = repository
            .list_events_needing_link_check(stale_before, self.config.batch_size as i64)
            .await?;

        if events.is_empty() {
            debug!(
                stale_before = %stale_before,
                batch_size = self.config.batch_size,
                "major event link check skipped: no stale targets"
            );
            return Ok(LinkCheckResult::default());
        }

        let checked_at = (self.now)();
        let mut result = LinkCheckResult::default();
        let mut checks = stream::iter(events.into_iter().map(|event| async move {
            let (link_status, check_error) = self.check_link(&event.link).await;
            let update = repository
                .update_event_link_status(event.id, link_status.clone(), checked_at)
                .await;

            (event, link_status, check_error, update)
        }))
        .buffer_unordered(self.config.max_concurrency.max(1));

        while let Some((event, link_status, check_error, update_result)) = checks.next().await {
            if let Err(err) = update_result {
                warn!(
                    event_id = event.id,
                    error = %err,
                    "failed to persist link check result"
                );
                continue;
            }

            result.checked += 1;
            match link_status {
                MajorEventLinkStatus::Ok => result.ok += 1,
                MajorEventLinkStatus::Blocked => result.blocked += 1,
                _ => result.failed += 1,
            }

            if let Some(err_msg) = check_error {
                debug!(
                    event_id = event.id,
                    status = %link_status,
                    link = redact_url_for_log(&event.link),
                    error = %err_msg,
                    "major event link check failed"
                );
            }
        }

        Ok(result)
    }

    pub async fn check_link(&self, raw_url: &str) -> (MajorEventLinkStatus, Option<String>) {
        let trimmed = raw_url.trim();
        if trimmed.is_empty() {
            return (
                MajorEventLinkStatus::Failed,
                Some("link is empty".to_string()),
            );
        }

        let parsed = match Url::parse(trimmed) {
            Ok(url) => url,
            Err(err) => {
                return (
                    MajorEventLinkStatus::Failed,
                    Some(format!("parse link: {err}")),
                );
            }
        };

        if !matches!(
            parsed.scheme().to_ascii_lowercase().as_str(),
            "http" | "https"
        ) {
            return (
                MajorEventLinkStatus::Blocked,
                Some(format!("unsupported link scheme: {}", parsed.scheme())),
            );
        }

        let head_status = self.probe_url(parsed.as_str(), reqwest::Method::HEAD).await;
        if let Ok((status, _final_url)) = &head_status {
            if is_success_status(*status) {
                return (MajorEventLinkStatus::Ok, None);
            }

            if !should_fallback_to_get(status.as_u16(), None) {
                return (
                    MajorEventLinkStatus::Failed,
                    Some(format!("head status code: {}", status.as_u16())),
                );
            }
        } else if let Err(head_err) = &head_status {
            match head_err {
                ProbeError::Blocked(reason) => {
                    return (MajorEventLinkStatus::Blocked, Some(reason.clone()));
                }
                ProbeError::Failed(reason)
                    if should_skip_get_fallback_on_head_error(reason)
                        || !should_fallback_to_get(0, Some(reason.as_str())) =>
                {
                    return (
                        MajorEventLinkStatus::Failed,
                        Some(format!("head request failed: {reason}")),
                    );
                }
                ProbeError::Failed(_) => {}
            }
        }

        let get_status = self.probe_url(parsed.as_str(), reqwest::Method::GET).await;
        match get_status {
            Ok((status, _final_url)) => {
                if is_success_status(status) {
                    return (MajorEventLinkStatus::Ok, None);
                }

                (
                    MajorEventLinkStatus::Failed,
                    Some(format!("get status code: {}", status.as_u16())),
                )
            }
            Err(ProbeError::Blocked(reason)) => (MajorEventLinkStatus::Blocked, Some(reason)),
            Err(ProbeError::Failed(get_err)) => match head_status {
                Err(ProbeError::Failed(head_err)) => (
                    MajorEventLinkStatus::Failed,
                    Some(format!("head/get failed: {head_err}; {get_err}")),
                ),
                _ => (
                    MajorEventLinkStatus::Failed,
                    Some(format!("get request failed: {get_err}")),
                ),
            },
        }
    }

    async fn validate_url_redirect(
        &self,
        original: &Url,
        final_url: &str,
    ) -> Result<(), ScraperError> {
        if original.as_str() == final_url {
            return self.validate_host(original).await;
        }

        let parsed_final =
            Url::parse(final_url).map_err(|err| ScraperError::LinkFailed(err.to_string()))?;
        if !matches!(
            parsed_final.scheme().to_ascii_lowercase().as_str(),
            "http" | "https"
        ) {
            return Err(ScraperError::LinkBlocked(format!(
                "unsupported redirect scheme: {}",
                parsed_final.scheme()
            )));
        }

        let original_host = original.host_str();
        let final_host = parsed_final.host_str();
        if final_host.is_none() {
            return Err(ScraperError::LinkBlocked(
                "missing host in final redirect url".to_string(),
            ));
        }
        if original_host.is_none() {
            return Err(ScraperError::LinkBlocked(
                "missing host in original redirect url".to_string(),
            ));
        }

        self.validate_host(&parsed_final).await
    }

    async fn probe_url(
        &self,
        parsed_url: &str,
        method: reqwest::Method,
    ) -> Result<(reqwest::StatusCode, String), ProbeError> {
        let max_redirect_hops = self.config.max_redirect_hops.max(1);
        let mut current =
            Url::parse(parsed_url).map_err(|err| ProbeError::Failed(err.to_string()))?;
        let mut visited_urls = HashSet::new();
        visited_urls.insert(current.as_str().to_string());
        let mut hop = 0usize;

        loop {
            self.validate_host(&current)
                .await
                .map_err(Self::to_probe_error)?;

            let HttpProbeResponse {
                status,
                response_url,
                redirect_location,
            } = self
                .http_client
                .probe(method.clone(), current.as_str(), self.config.timeout)
                .await
                .map_err(ProbeError::Failed)?;

            let parsed_response_url =
                Url::parse(&response_url).map_err(|err| ProbeError::Failed(err.to_string()))?;
            self.validate_url_redirect(&current, parsed_response_url.as_str())
                .await
                .map_err(Self::to_probe_error)?;

            if !status.is_redirection() {
                return Ok((status, parsed_response_url.to_string()));
            }

            let Some(location) = redirect_location else {
                return Ok((status, parsed_response_url.to_string()));
            };
            let next = parsed_response_url
                .join(&location)
                .map_err(|err| ProbeError::Failed(err.to_string()))?;

            self.validate_url_redirect(&parsed_response_url, next.as_str())
                .await
                .map_err(Self::to_probe_error)?;

            if hop >= max_redirect_hops {
                return Err(ProbeError::Failed(format!(
                    "too many redirects (> {}) for {parsed_url}",
                    max_redirect_hops
                )));
            }

            let next_key = next.as_str().to_string();
            if !visited_urls.insert(next_key.clone()) {
                return Err(ProbeError::Failed(format!(
                    "redirect loop detected for {next_key}"
                )));
            }
            current = next;
            hop += 1;
        }
    }

    async fn validate_host(&self, parsed_url: &Url) -> Result<(), ScraperError> {
        let host = parsed_url
            .host_str()
            .ok_or_else(|| ScraperError::LinkBlocked("missing host in url".to_string()))?;

        if is_blocked_hostname(host) {
            return Err(ScraperError::LinkBlocked(format!(
                "blocked hostname: {host}"
            )));
        }

        if let Some(ip) = parsed_url.host().and_then(host_to_ip)
            && is_private_or_local_ip(ip)
        {
            return Err(ScraperError::LinkBlocked(format!(
                "blocked direct ip host: {ip}"
            )));
        }

        let port = parsed_url.port_or_known_default().unwrap_or(443);
        let resolved = self
            .host_resolver
            .resolve(host, port)
            .await
            .map_err(|err| ScraperError::LinkFailed(format!("resolve host {host}: {err}")))?;

        if resolved.is_empty() {
            return Err(ScraperError::LinkFailed(format!(
                "host {host} resolved to no ip"
            )));
        }

        let mut unique_ips = HashSet::new();
        for ip in resolved {
            if !unique_ips.insert(ip) {
                continue;
            }

            if is_private_or_local_ip(ip) {
                return Err(ScraperError::LinkBlocked(format!(
                    "blocked resolved ip {ip} for host {host}"
                )));
            }
        }

        Ok(())
    }

    fn to_probe_error(err: ScraperError) -> ProbeError {
        match err {
            ScraperError::LinkBlocked(reason) => ProbeError::Blocked(reason),
            other => ProbeError::Failed(other.to_string()),
        }
    }
}

mod url_safety;
use url_safety::{
    host_to_ip, is_blocked_hostname, is_private_or_local_ip, is_success_status, redact_url_for_log,
    should_fallback_to_get,
};

fn should_skip_get_fallback_on_head_error(reason: &str) -> bool {
    reason.contains("redirect loop detected") || reason.contains("too many redirects")
}

#[cfg(test)]
mod tests;
