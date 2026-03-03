use std::collections::HashSet;

use scraper_core::error::ScraperError;
use url::Url;

use super::{
    LinkChecker,
    transport::HttpProbeResponse,
    url_safety::{host_to_ip, is_blocked_hostname, is_private_or_local_ip},
};

#[derive(Debug)]
pub(super) enum ProbeError {
    Blocked(String),
    Failed(String),
}

impl LinkChecker {
    pub(super) async fn validate_url_redirect(
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
                "missing host in final redirect url".to_owned(),
            ));
        }
        if original_host.is_none() {
            return Err(ScraperError::LinkBlocked(
                "missing host in original redirect url".to_owned(),
            ));
        }

        self.validate_host(&parsed_final).await
    }

    pub(super) async fn probe_url(
        &self,
        parsed_url: &str,
        method: reqwest::Method,
    ) -> Result<(reqwest::StatusCode, String), ProbeError> {
        let max_redirect_hops = self.config.max_redirect_hops.max(1);
        let mut current =
            Url::parse(parsed_url).map_err(|err| ProbeError::Failed(err.to_string()))?;
        let mut visited_urls = HashSet::new();
        visited_urls.insert(current.as_str().to_owned());
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

            let next_key = next.as_str().to_owned();
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
            .ok_or_else(|| ScraperError::LinkBlocked("missing host in url".to_owned()))?;

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
            other @ (ScraperError::Http(_)
            | ScraperError::HttpStatus { .. }
            | ScraperError::XmlParse(_)
            | ScraperError::Database(_)
            | ScraperError::Config(_)
            | ScraperError::AllFeedsFailed(_)
            | ScraperError::LinkFailed(_)
            | ScraperError::Io(_)) => ProbeError::Failed(other.to_string()),
        }
    }
}

pub(super) fn should_skip_get_fallback_on_head_error(reason: &str) -> bool {
    reason.contains("redirect loop detected") || reason.contains("too many redirects")
}
