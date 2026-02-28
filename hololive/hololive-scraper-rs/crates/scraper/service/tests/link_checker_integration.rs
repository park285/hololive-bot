use std::time::Duration;

use reqwest::{Client, StatusCode, header::RANGE};
use scraper_core::model::MajorEventLinkStatus;
use scraper_service::link_checker::{LinkChecker, LinkCheckerConfig};

const TARGET_URL: &str = "https://hololive.hololivepro.com/events/feed/";

fn is_success_status(status: StatusCode) -> bool {
    status.is_success() || status.is_redirection()
}

fn is_head_fallback_status(status: StatusCode) -> bool {
    matches!(
        status.as_u16(),
        400 | 401 | 403 | 405 | 406 | 429 | 501 | 502 | 503 | 504
    )
}

fn test_client() -> Client {
    Client::builder()
        .user_agent(
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) \
             AppleWebKit/537.36 (KHTML, like Gecko) \
             Chrome/133.0.0.0 Safari/537.36",
        )
        // SSRF 우회 방지: redirect 비활성화 (3xx 직접 수신 → is_success_status에서 OK 판정)
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .expect("failed to build reqwest client")
}

#[tokio::test]
#[ignore = "requires outbound network access to hololive.hololivepro.com"]
async fn real_url_head_and_get_probe() {
    let client = test_client();

    let head_resp = client
        .head(TARGET_URL)
        .timeout(Duration::from_secs(8))
        .send()
        .await
        .expect("HEAD request failed");
    let head_status = head_resp.status();

    assert!(
        is_success_status(head_status) || is_head_fallback_status(head_status),
        "unexpected HEAD status: {head_status}"
    );

    let get_resp = client
        .get(TARGET_URL)
        .header(RANGE, "bytes=0-0")
        .timeout(Duration::from_secs(8))
        .send()
        .await
        .expect("GET request failed");
    let get_status = get_resp.status();

    assert!(
        is_success_status(get_status),
        "unexpected GET status: {get_status}"
    );
}

#[tokio::test]
#[ignore = "requires outbound network access to hololive.hololivepro.com"]
async fn link_checker_real_url_check_link_returns_ok() {
    let checker = LinkChecker::new(
        test_client(),
        LinkCheckerConfig {
            timeout: Duration::from_secs(8),
            ..LinkCheckerConfig::default()
        },
    );

    let (status, err) = checker.check_link(TARGET_URL).await;

    assert_eq!(
        status,
        MajorEventLinkStatus::Ok,
        "expected ok status for real URL, error: {:?}",
        err
    );
}
