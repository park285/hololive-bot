# Phase 2: LinkChecker

> [← 메인 계획서](IMPLEMENTATION_PLAN.md)

## 1. 모듈 구조

`scraper-service/link_checker.rs`에 전체 구현. Repository 확장은 `scraper-infra/repository.rs`에 메서드 추가.

## 2. LinkChecker 구현

**파일**: `crates/scraper/service/src/link_checker.rs`

### 2.1 Struct 정의

```rust
use std::net::{IpAddr, Ipv4Addr};
use std::sync::Arc;
use std::time::Duration;

use reqwest::Client;
use scraper_core::model::{MajorEvent, MajorEventLinkStatus};
use scraper_infra::repository::Repository;

pub struct LinkCheckerConfig {
    pub request_timeout: Duration,
    pub stale_after: Duration,
    pub batch_size: i64,
    pub user_agent: String,
}

impl Default for LinkCheckerConfig {
    fn default() -> Self {
        Self {
            request_timeout: Duration::from_secs(8),
            stale_after: Duration::from_secs(72 * 3600), // 72h
            batch_size: 200,
            user_agent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36".into(),
        }
    }
}

#[derive(Debug, Default)]
pub struct LinkCheckResult {
    pub checked: u32,
    pub ok: u32,
    pub failed: u32,
    pub blocked: u32,
}

pub struct LinkChecker {
    http_client: Client,
    repository: Arc<Repository>,
    config: LinkCheckerConfig,
}
```

### 2.2 Host Validation (Private IP)

```rust
/// Private/Local IP 범위 체크 (Go isPrivateOrLocalIP 동치)
fn is_private_or_local_ip(ip: IpAddr) -> bool {
    match ip {
        IpAddr::V4(v4) => {
            // Loopback: 127.0.0.0/8
            if v4.is_loopback() { return true; }
            // Private: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
            if v4.is_private() { return true; }
            // Link-local: 169.254.0.0/16
            if v4.is_link_local() { return true; }
            // Multicast: 224.0.0.0/4
            if v4.is_multicast() { return true; }
            // Unspecified: 0.0.0.0
            if v4.is_unspecified() { return true; }
            // Carrier-grade NAT: 100.64.0.0/10
            let octets = v4.octets();
            if octets[0] == 100 && (octets[1] & 0xC0) == 0x40 {
                return true;
            }
            false
        }
        IpAddr::V6(v6) => {
            v6.is_loopback()
                || v6.is_multicast()
                || v6.is_unspecified()
                // link-local: fe80::/10
                || (v6.segments()[0] & 0xffc0) == 0xfe80
        }
    }
}

/// 차단 호스트명 패턴 (Go isBlockedHostname 동치)
fn is_blocked_hostname(host: &str) -> bool {
    let h = host.to_lowercase();
    h == "localhost"
        || h.ends_with(".localhost")
        || h.ends_with(".local")
}

/// 호스트 DNS 해석 후 private IP 체크
async fn validate_host(host: &str) -> Result<(), LinkValidationError> {
    let normalized = host.to_lowercase();
    if normalized.is_empty() {
        return Err(LinkValidationError::EmptyHost);
    }
    if is_blocked_hostname(&normalized) {
        return Err(LinkValidationError::Blocked(
            format!("blocked hostname: {}", normalized),
        ));
    }

    // 직접 IP인 경우
    if let Ok(ip) = normalized.parse::<IpAddr>() {
        if is_private_or_local_ip(ip) {
            return Err(LinkValidationError::Blocked(
                format!("blocked ip: {}", normalized),
            ));
        }
        return Ok(());
    }

    // DNS 해석
    let addrs = tokio::net::lookup_host(format!("{}:0", normalized))
        .await
        .map_err(|e| LinkValidationError::DnsError(e.to_string()))?;

    let ips: Vec<_> = addrs.map(|a| a.ip()).collect();
    if ips.is_empty() {
        return Err(LinkValidationError::DnsError(
            format!("no IP for host: {}", normalized),
        ));
    }

    for ip in &ips {
        if is_private_or_local_ip(*ip) {
            return Err(LinkValidationError::Blocked(
                format!("blocked resolved ip {} for host {}", ip, normalized),
            ));
        }
    }

    Ok(())
}

#[derive(Debug)]
enum LinkValidationError {
    EmptyHost,
    Blocked(String),
    DnsError(String),
}
```

### 2.3 HEAD -> GET Fallback

```rust
impl LinkChecker {
    pub async fn check_stale_links(&self) -> Result<LinkCheckResult, scraper_core::error::ScraperError> {
        let stale_before = chrono::Utc::now() - chrono::Duration::from_std(self.config.stale_after).unwrap();
        let events = self.repository
            .list_events_needing_link_check(stale_before, self.config.batch_size)
            .await?;

        let mut result = LinkCheckResult::default();

        for event in &events {
            let (status, _err) = self.check_link(&event.link).await;
            self.repository
                .update_event_link_status(event.id, status.clone(), chrono::Utc::now())
                .await?;

            result.checked += 1;
            match status {
                MajorEventLinkStatus::Ok => result.ok += 1,
                MajorEventLinkStatus::Blocked => result.blocked += 1,
                _ => result.failed += 1,
            }
        }

        Ok(result)
    }

    async fn check_link(&self, raw_url: &str) -> (MajorEventLinkStatus, Option<String>) {
        let trimmed = raw_url.trim();
        if trimmed.is_empty() {
            return (MajorEventLinkStatus::Failed, Some("empty link".into()));
        }

        let parsed = match url::Url::parse(trimmed) {
            Ok(u) => u,
            Err(e) => return (MajorEventLinkStatus::Failed, Some(e.to_string())),
        };

        let scheme = parsed.scheme();
        if scheme != "http" && scheme != "https" {
            return (MajorEventLinkStatus::Blocked, Some(format!("unsupported scheme: {}", scheme)));
        }

        if let Some(host) = parsed.host_str() {
            if let Err(e) = validate_host(host).await {
                return match e {
                    LinkValidationError::Blocked(reason) => (MajorEventLinkStatus::Blocked, Some(reason)),
                    _ => (MajorEventLinkStatus::Failed, Some(format!("{:?}", e))),
                };
            }
        }

        // HEAD probe
        let head_result = self.probe_url(parsed.as_str(), reqwest::Method::HEAD).await;
        if let Ok(code) = head_result {
            if is_success_status(code) {
                return (MajorEventLinkStatus::Ok, None);
            }
            if !should_fallback_to_get(code) {
                return (MajorEventLinkStatus::Failed, Some(format!("HEAD status: {}", code)));
            }
        }

        // GET fallback
        let get_result = self.probe_url(parsed.as_str(), reqwest::Method::GET).await;
        match get_result {
            Ok(code) if is_success_status(code) => (MajorEventLinkStatus::Ok, None),
            Ok(code) => (MajorEventLinkStatus::Failed, Some(format!("GET status: {}", code))),
            Err(e) => (MajorEventLinkStatus::Failed, Some(e)),
        }
    }

    async fn probe_url(&self, url: &str, method: reqwest::Method) -> Result<u16, String> {
        let mut request = self.http_client
            .request(method.clone(), url)
            .header("User-Agent", &self.config.user_agent)
            .timeout(self.config.request_timeout);

        if method == reqwest::Method::GET {
            request = request.header("Range", "bytes=0-0");
        }

        let resp = request
            .send()
            .await
            .map_err(|e| e.to_string())?;

        Ok(resp.status().as_u16())
    }
}

fn is_success_status(code: u16) -> bool {
    (200..400).contains(&code)
}

/// HEAD 실패 시 GET fallback 대상 상태 코드 (Go shouldFallbackToGet 동치)
fn should_fallback_to_get(code: u16) -> bool {
    matches!(
        code,
        400 | 401 | 403 | 405 | 406 | 429 | 501 | 502 | 503 | 504
    )
}
```

## 3. Repository 확장

**`scraper-infra/repository.rs`에 추가**:

```rust
impl Repository {
    /// 링크 재검증 대상 조회
    pub async fn list_events_needing_link_check(
        &self,
        stale_before: DateTime<Utc>,
        limit: i64,
    ) -> Result<Vec<MajorEvent>, sqlx::Error> {
        let limit = if limit <= 0 { 100 } else { limit };

        let rows: Vec<MajorEventRow> = sqlx::query_as(
            r#"
            SELECT id, external_id, type, title, link,
                   COALESCE(description, '') as description,
                   COALESCE(members, '{}') as members,
                   pub_date, event_start_date, event_end_date,
                   status,
                   COALESCE(link_status, 'unchecked') as link_status,
                   link_checked_at, notified_at,
                   COALESCE(notified_week, '') as notified_week,
                   COALESCE(notified_month, '') as notified_month,
                   created_at, updated_at
            FROM major_events
            WHERE status = 'active'
              AND COALESCE(link, '') <> ''
              AND (link_checked_at IS NULL OR link_checked_at < $1)
            ORDER BY link_checked_at ASC NULLS FIRST, updated_at DESC
            LIMIT $2
            "#,
        )
        .bind(stale_before)
        .bind(limit)
        .fetch_all(&self.pool)
        .await?;

        Ok(rows.into_iter().map(MajorEvent::from).collect())
    }

    /// 링크 검증 결과 업데이트
    pub async fn update_event_link_status(
        &self,
        event_id: i32,
        status: MajorEventLinkStatus,
        checked_at: DateTime<Utc>,
    ) -> Result<(), sqlx::Error> {
        sqlx::query(
            r#"
            UPDATE major_events
            SET link_status = $1,
                link_checked_at = $2,
                updated_at = NOW()
            WHERE id = $3
            "#,
        )
        .bind(status.as_str())
        .bind(checked_at)
        .bind(event_id)
        .execute(&self.pool)
        .await?;

        Ok(())
    }
}
```

## 4. ScraperScheduler 통합

`scheduler.rs`의 `run_scrape()`에 이미 LinkChecker 호출이 포함되어 있다 (3.7절 참조).
순서: `update_expired_events` -> `scrape_and_store` -> `check_stale_links`

## 5. Phase 2 체크리스트

- [x] `link_checker.rs` -- `LinkChecker` struct + `LinkCheckerConfig`
- [x] `link_checker.rs` -- `is_private_or_local_ip()` (loopback, private, link-local, multicast, carrier-grade NAT)
- [x] `link_checker.rs` -- `is_blocked_hostname()` (localhost, *.localhost, *.local)
- [x] `link_checker.rs` -- `validate_host()` DNS 해석 + private IP 체크
- [x] `link_checker.rs` -- `check_link()` HEAD probe + GET fallback
- [x] `link_checker.rs` -- `should_fallback_to_get()` (400,401,403,405,406,429,501,502,503,504)
- [x] `link_checker.rs` -- `is_success_status()` (200..400)
- [x] `link_checker.rs` -- GET 요청 시 `Range: bytes=0-0` 헤더
- [x] `repository.rs` -- `list_events_needing_link_check()` SQL
- [x] `repository.rs` -- `update_event_link_status()` SQL
- [x] `scheduler.rs` -- `run_scrape()`에 `check_stale_links()` 통합 확인
- [x] 단위 테스트 9개 포팅 (Go link_checker_test.go)
- [x] 통합 테스트: 실제 URL (hololive.hololivepro.com) 대상 HEAD/GET 검증
- [x] `cargo test --all` 전체 통과
- [x] Docker 빌드 + 실행 검증

## 6. 검증 계획

1. **Private IP 블로킹 테스트**: 127.0.0.1, 10.x, 172.16.x, 192.168.x, 100.64.x, ::1 대상 `Blocked` 반환 확인
2. **HEAD -> GET Fallback 테스트**: mock server로 HEAD 405 응답 -> GET 200 성공 경로 검증
3. **Blocked hostname 테스트**: localhost, foo.localhost, bar.local -> Blocked
4. **실제 링크 검증**: `hololive.hololivepro.com/events/...` 5건에 대해 Go/Rust 양쪽 실행 결과 비교
