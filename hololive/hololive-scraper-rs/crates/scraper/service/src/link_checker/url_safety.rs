use ipnet::IpNet;
use std::{net::IpAddr, sync::LazyLock};
use url::Url;

static DENY_IP_RANGES: LazyLock<Vec<IpNet>> = LazyLock::new(|| {
    const CIDR_BLOCKS: &[&str] = &[
        // IPv4
        "0.0.0.0/8",
        "10.0.0.0/8",
        "100.64.0.0/10",
        "127.0.0.0/8",
        "169.254.0.0/16",
        "172.16.0.0/12",
        "192.0.0.0/24",
        "192.0.2.0/24",
        "192.168.0.0/16",
        "198.18.0.0/15",
        "198.51.100.0/24",
        "203.0.113.0/24",
        "224.0.0.0/4",
        "240.0.0.0/4",
        // IPv6
        "::/128",
        "::1/128",
        "::ffff:0:0/96",
        "100::/64",
        "2001:db8::/32",
        "2001:10::/28",
        "fc00::/7",
        "fe80::/10",
        "ff00::/8",
    ];

    CIDR_BLOCKS
        .iter()
        .map(|cidr| cidr.parse::<IpNet>().expect("invalid hardcoded CIDR block"))
        .collect()
});

pub(super) fn should_fallback_to_get(status_code: u16, req_err: Option<&str>) -> bool {
    if req_err.is_some() {
        return true;
    }

    matches!(
        status_code,
        400 | 401 | 403 | 405 | 406 | 429 | 501 | 502 | 503 | 504
    )
}

pub(super) fn is_success_status(status: reqwest::StatusCode) -> bool {
    status.is_success() || status.is_redirection()
}

pub(super) fn redact_url_for_log(raw: &str) -> String {
    match Url::parse(raw) {
        Ok(mut parsed) => {
            parsed.set_query(None);
            parsed.set_fragment(None);
            parsed.to_string()
        }
        Err(_) => raw.split('?').next().unwrap_or(raw).to_string(),
    }
}

pub(super) fn is_blocked_hostname(hostname: &str) -> bool {
    let host = hostname.to_ascii_lowercase();
    host == "localhost" || host.ends_with(".localhost") || host.ends_with(".local")
}

pub(super) fn is_private_or_local_ip(ip: IpAddr) -> bool {
    if DENY_IP_RANGES.iter().any(|range| range.contains(&ip)) {
        return true;
    }

    match ip {
        IpAddr::V6(v6) => v6
            .to_ipv4_mapped()
            .map(IpAddr::V4)
            .is_some_and(|mapped| DENY_IP_RANGES.iter().any(|range| range.contains(&mapped))),
        IpAddr::V4(_) => false,
    }
}

pub(super) fn host_to_ip(host: url::Host<&str>) -> Option<IpAddr> {
    match host {
        url::Host::Ipv4(ipv4) => Some(IpAddr::V4(ipv4)),
        url::Host::Ipv6(ipv6) => Some(IpAddr::V6(ipv6)),
        url::Host::Domain(_) => None,
    }
}

#[cfg(test)]
mod tests {
    use super::is_private_or_local_ip;
    use std::net::IpAddr;

    #[test]
    fn blocks_ipv4_private_and_reserved_ranges() {
        let blocked = [
            "10.0.0.1",
            "100.64.0.1",
            "127.0.0.1",
            "169.254.10.20",
            "172.16.1.1",
            "192.168.0.10",
            "198.18.1.1",
            "203.0.113.10",
        ];

        for raw in blocked {
            let ip: IpAddr = raw.parse().expect("valid ip");
            assert!(is_private_or_local_ip(ip), "expected blocked ip: {raw}");
        }
    }

    #[test]
    fn blocks_ipv6_local_and_documentation_ranges() {
        let blocked = [
            "::",
            "::1",
            "fc00::1",
            "fe80::1",
            "ff02::1",
            "2001:db8::1",
            "::ffff:127.0.0.1",
        ];

        for raw in blocked {
            let ip: IpAddr = raw.parse().expect("valid ip");
            assert!(is_private_or_local_ip(ip), "expected blocked ip: {raw}");
        }
    }

    #[test]
    fn allows_public_ips() {
        let allowed = ["93.184.216.34", "2606:4700:4700::1111"];

        for raw in allowed {
            let ip: IpAddr = raw.parse().expect("valid ip");
            assert!(!is_private_or_local_ip(ip), "expected allowed ip: {raw}");
        }
    }
}
