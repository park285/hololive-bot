#!/usr/bin/env python3
"""Validate the exact, deliberately narrow Trivy exception tuples."""

from __future__ import annotations

import argparse
import json
import re
from dataclasses import dataclass
from pathlib import Path


EXPIRY = "2026-09-14"


@dataclass(frozen=True)
class Entry:
    purls: tuple[str, ...]
    statement: str
    expired_at: str


STATEMENTS = {
    "nginx": (
        "Owner: hololive-bot. The exact admin-dashboard ingress image has no TLS or QUIC listener; "
        "OpenSSL classifies this as a QUIC-server-only Low-severity denial of service. Remove this "
        "exception when the official multi-arch Nginx image contains OpenSSL 3.5.8-r0 or later."
    ),
    "postgres": (
        "Owner: hololive-bot. Compose runs this exact image as 999:999, so its root-only gosu branch "
        "is unreachable; PostgreSQL does not instantiate an OpenSSL QUIC listener. Remove on a clean "
        "official image rebuild."
    ),
    "deunhealth": (
        "Owner: hololive-bot. Exact source 37e5bd4 was reviewed: govulncheck excludes the "
        "inventory-only calls, and the remaining reports require daemon-plugin or OTel baggage paths "
        "absent from this client-only runtime. Remove on a clean upstream rebuild."
    ),
    "socket-proxy": (
        "Owner: hololive-bot. Exact release source bcb95c8 passed govulncheck with no reachable "
        "vulnerabilities; Trivy only inventories the Go 1.26.5 binary. Remove on an upstream release "
        "built with a fixed Go toolchain."
    ),
}

EXPECTED_IDS = {
    "nginx": {"CVE-2026-14456"},
    "postgres": {
        "CVE-2025-61726", "CVE-2025-61729", "CVE-2025-68121", "CVE-2026-14456",
        "CVE-2026-25679", "CVE-2026-27145", "CVE-2026-32280", "CVE-2026-32281",
        "CVE-2026-32283", "CVE-2026-33811", "CVE-2026-33814", "CVE-2026-33818",
        "CVE-2026-39820", "CVE-2026-39821", "CVE-2026-39822", "CVE-2026-39836",
        "CVE-2026-42499", "CVE-2026-42504", "CVE-2026-56853", "CVE-2026-56858",
        "CVE-2026-56859", "CVE-2026-56860", "CVE-2026-56862",
    },
    "deunhealth": {
        "CVE-2025-61726", "CVE-2025-68121", "CVE-2026-25679", "CVE-2026-25681",
        "CVE-2026-27136", "CVE-2026-27145", "CVE-2026-29181", "CVE-2026-32280",
        "CVE-2026-32281", "CVE-2026-32283", "CVE-2026-33811", "CVE-2026-33814",
        "CVE-2026-33818", "CVE-2026-34040", "CVE-2026-39820", "CVE-2026-39821",
        "CVE-2026-39822", "CVE-2026-39836", "CVE-2026-41567", "CVE-2026-42306",
        "CVE-2026-42499", "CVE-2026-42504", "CVE-2026-46600", "CVE-2026-56853",
        "CVE-2026-56858", "CVE-2026-56859", "CVE-2026-56860", "CVE-2026-56862",
    },
    "socket-proxy": {
        "CVE-2026-33818", "CVE-2026-39821", "CVE-2026-46600", "CVE-2026-56853",
        "CVE-2026-56858", "CVE-2026-56859", "CVE-2026-56860", "CVE-2026-56862",
    },
}

STDLIB_DEUNHEALTH = "pkg:golang/stdlib@v1.25.5"
STDLIB_POSTGRES = "pkg:golang/stdlib@v1.24.6"
STDLIB_SOCKET_PROXY = "pkg:golang/stdlib@v1.26.5"
X_NET_DEUNHEALTH = "pkg:golang/golang.org/x/net@v0.47.0"


def expected_purls(key: str, cve: str) -> tuple[str, ...]:
    if key == "nginx" or (key == "postgres" and cve == "CVE-2026-14456"):
        return (
            "pkg:apk/alpine/libcrypto3@3.5.7-r0",
            "pkg:apk/alpine/libssl3@3.5.7-r0",
        )
    if key == "postgres":
        return (STDLIB_POSTGRES,)
    if key == "socket-proxy":
        return (STDLIB_SOCKET_PROXY,)
    if cve in {"CVE-2026-25681", "CVE-2026-27136", "CVE-2026-46600"}:
        return (X_NET_DEUNHEALTH,)
    if cve in {"CVE-2026-33814", "CVE-2026-39821"}:
        return (STDLIB_DEUNHEALTH, X_NET_DEUNHEALTH)
    if cve == "CVE-2026-29181":
        return ("pkg:golang/go.opentelemetry.io/otel@v1.39.0",)
    if cve == "CVE-2026-34040":
        return ("pkg:golang/github.com/moby/moby@v28.5.2%2Bincompatible",)
    if cve in {"CVE-2026-41567", "CVE-2026-42306"}:
        return (
            "pkg:golang/github.com/docker/docker@v28.5.2%2Bincompatible",
            "pkg:golang/github.com/moby/moby@v28.5.2%2Bincompatible",
        )
    return (STDLIB_DEUNHEALTH,)


def scalar(value: str) -> str:
    if value.startswith('"'):
        parsed = json.loads(value)
        if not isinstance(parsed, str):
            raise ValueError("quoted scalar is not a string")
        return parsed
    if not value or value != value.strip():
        raise ValueError(f"invalid scalar: {value!r}")
    return value


def parse_entries(path: Path) -> dict[str, Entry]:
    lines = path.read_text(encoding="utf-8").splitlines()
    index = 0
    while index < len(lines) and (not lines[index] or lines[index].startswith("#")):
        index += 1
    if index >= len(lines) or lines[index] != "vulnerabilities:":
        raise ValueError("missing vulnerabilities root")
    index += 1
    entries: dict[str, Entry] = {}
    while index < len(lines):
        match = re.fullmatch(r"  - id: (CVE-[0-9]+-[0-9]+)", lines[index])
        if match is None:
            raise ValueError(f"line {index + 1}: expected exact CVE entry")
        cve = match.group(1)
        index += 1
        if index >= len(lines) or lines[index] != "    purls:":
            raise ValueError(f"{cve}: missing purls")
        index += 1
        purls: list[str] = []
        while index < len(lines) and lines[index].startswith("      - "):
            purls.append(scalar(lines[index][8:]))
            index += 1
        if not purls:
            raise ValueError(f"{cve}: purls must not be empty")
        if index >= len(lines) or lines[index] != "    statement: >-":
            raise ValueError(f"{cve}: missing statement")
        index += 1
        statement_lines: list[str] = []
        while index < len(lines) and lines[index].startswith("      "):
            statement_lines.append(lines[index][6:])
            index += 1
        if not statement_lines:
            raise ValueError(f"{cve}: statement must not be empty")
        if index >= len(lines) or not lines[index].startswith("    expired_at: "):
            raise ValueError(f"{cve}: missing expiry")
        expired_at = scalar(lines[index][16:])
        index += 1
        if cve in entries:
            raise ValueError(f"duplicate vulnerability: {cve}")
        entries[cve] = Entry(tuple(sorted(purls)), " ".join(statement_lines), expired_at)
    return entries


def validate(root: Path) -> None:
    ci_dir = root / "scripts" / "ci"
    for key, expected_ids in EXPECTED_IDS.items():
        path = ci_dir / f"trivyignore-{key}.yaml"
        entries = parse_entries(path)
        if set(entries) != expected_ids:
            raise ValueError(f"{key}: CVE allowlist mismatch")
        for cve, entry in entries.items():
            expected = Entry(
                tuple(sorted(expected_purls(key, cve))),
                STATEMENTS[key],
                EXPIRY,
            )
            if entry != expected:
                raise ValueError(f"{key} {cve}: exception tuple mismatch")

    compose = (root / "deploy" / "compose" / "docker-compose.prod.yml").read_text(encoding="utf-8")
    compose_sections = compose.split("\nservices:\n", maxsplit=1)
    if len(compose_sections) != 2:
        raise ValueError("missing exact Compose services root")
    postgres_match = re.search(
        r"(?ms)^  holo-postgres:\n(?P<body>.*?)(?=^  [a-zA-Z0-9_-]+:\n)",
        compose_sections[1],
    )
    if postgres_match is None:
        raise ValueError("missing holo-postgres service")
    postgres_users = re.findall(r'^    user: (.+)$', postgres_match.group("body"), re.MULTILINE)
    if postgres_users != ['"999:999"']:
        raise ValueError('holo-postgres exception requires exact user: "999:999"')

    ingress = (root / "deploy" / "nginx" / "admin-dashboard-ingress.conf.template").read_text(
        encoding="utf-8"
    )
    active_ingress = "\n".join(line.split("#", 1)[0] for line in ingress.splitlines())
    forbidden_directive = re.compile(
        r"(?im)^\s*(?:listen\s+[^;]*(?:\bssl\b|\bquic\b)|ssl_|quic_|http3\b|include\b)"
    )
    if forbidden_directive.search(active_ingress):
        raise ValueError("nginx exception requires an include-free config with no TLS or QUIC directives")

    live_compose = (
        root / "deploy" / "compose" / "docker-compose.live-compat.yml"
    ).read_text(encoding="utf-8")
    command = (
        'command: ["nginx", "-c", "/etc/nginx/admin-dashboard-ingress.conf", '
        '"-g", "daemon off;"]'
    )
    if command not in live_compose:
        raise ValueError("nginx exception requires the reviewed ingress config as its exact command target")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    args = parser.parse_args()
    try:
        validate(args.root.resolve())
    except (OSError, ValueError, json.JSONDecodeError) as error:
        parser.error(str(error))
    print("exact Trivy exception tuples passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
