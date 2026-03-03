use scraper_core::model::MajorEventLinkStatus;
use url::Url;

use super::{
    LinkChecker,
    probe::{ProbeError, should_skip_get_fallback_on_head_error},
    url_safety::{is_success_status, should_fallback_to_get},
};

impl LinkChecker {
    pub async fn check_link(&self, raw_url: &str) -> (MajorEventLinkStatus, Option<String>) {
        let trimmed = raw_url.trim();
        if trimmed.is_empty() {
            return (
                MajorEventLinkStatus::Failed,
                Some("link is empty".to_owned()),
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
}
