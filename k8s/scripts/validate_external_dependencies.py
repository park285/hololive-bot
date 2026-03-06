#!/usr/bin/env python3
"""Validate hololive k8s external dependency routing consistency.

Checks:
1) External service port mapping consistency.
2) DNS(ExternalName) vs IP(EndpointSlice) mode integrity.
3) ConfigMap URL/host/port consistency.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import yaml

EXPECTED_PORTS = {
    "holo-postgres": 5433,
    "iris": 3000,
    "cliproxy": 8787,
    "game-bot-twentyq": 30081,
    "game-bot-turtle": 30082,
}

URL_KEYS = {
    "IRIS_BASE_URL": ("iris", 3000),
    "SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL": ("game-bot-twentyq", 30081),
    "SERVICES_GAME_BOT_TURTLE_HEALTH_URL": ("game-bot-turtle", 30082),
}


@dataclass
class ValidationContext:
    errors: list[str]
    checks: list[str]

    def ok(self, message: str) -> None:
        self.checks.append(message)

    def error(self, message: str) -> None:
        self.errors.append(message)


def run_kustomize(overlay: str) -> str:
    cmd = ["kubectl", "kustomize", overlay, "--enable-helm"]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        stderr = result.stderr.strip() or "(no stderr)"
        raise RuntimeError(
            f"kubectl kustomize failed (overlay={overlay}): {stderr}"
        )
    return result.stdout


def parse_yaml_documents(rendered: str) -> list[dict[str, Any]]:
    docs: list[dict[str, Any]] = []
    for doc in yaml.safe_load_all(rendered):
        if isinstance(doc, dict):
            docs.append(doc)
    return docs


def parse_url(value: str, key: str, ctx: ValidationContext) -> tuple[str, int] | None:
    parsed = urlparse(value)
    if not parsed.scheme or not parsed.hostname:
        ctx.error(f"{key}: invalid URL '{value}'")
        return None
    if parsed.port is None:
        ctx.error(f"{key}: URL must include explicit port '{value}'")
        return None
    return parsed.hostname, parsed.port


def detect_mode(args_mode: str, services: dict[str, dict[str, Any]]) -> str:
    if args_mode != "auto":
        return args_mode

    postgres = services.get("holo-postgres")
    if not postgres:
        return "dns"

    spec = postgres.get("spec", {})
    if spec.get("type") == "ExternalName":
        return "dns"
    return "ip"


def expected_port_in_service(service: dict[str, Any], expected_port: int) -> bool:
    ports = service.get("spec", {}).get("ports", [])
    for item in ports:
        if item.get("port") == expected_port:
            return True
    return False


def endpoint_slice_has_port(slice_doc: dict[str, Any], expected_port: int) -> bool:
    for item in slice_doc.get("ports", []):
        if item.get("port") == expected_port:
            return True
    return False


def validate(
    *,
    mode: str,
    services: dict[str, dict[str, Any]],
    endpoint_slices: dict[str, list[dict[str, Any]]],
    configmap_data: dict[str, str] | None,
) -> ValidationContext:
    ctx = ValidationContext(errors=[], checks=[])

    for service_name, expected_port in EXPECTED_PORTS.items():
        service = services.get(service_name)
        if not service:
            ctx.error(f"missing Service: {service_name}")
            continue

        if not expected_port_in_service(service, expected_port):
            ctx.error(
                f"Service {service_name}: expected port {expected_port} not found"
            )
        else:
            ctx.ok(f"Service {service_name}: port {expected_port}")

        spec = service.get("spec", {})
        slices = endpoint_slices.get(service_name, [])

        if mode == "dns":
            if spec.get("type") != "ExternalName":
                ctx.error(
                    f"Service {service_name}: DNS mode requires type=ExternalName"
                )
            external_name = spec.get("externalName", "")
            if not external_name:
                ctx.error(
                    f"Service {service_name}: DNS mode requires non-empty externalName"
                )
            if slices:
                ctx.error(
                    f"Service {service_name}: DNS mode expects no EndpointSlice, found {len(slices)}"
                )
            else:
                ctx.ok(f"Service {service_name}: DNS mode EndpointSlice deleted")

        if mode == "ip":
            if spec.get("type") == "ExternalName":
                ctx.error(
                    f"Service {service_name}: IP mode must not use type=ExternalName"
                )
            if not slices:
                ctx.error(
                    f"Service {service_name}: IP mode requires EndpointSlice"
                )
                continue

            valid_slice_count = 0
            for slice_doc in slices:
                endpoints = slice_doc.get("endpoints", [])
                has_addresses = any(ep.get("addresses") for ep in endpoints)
                has_port = endpoint_slice_has_port(slice_doc, expected_port)
                if has_addresses and has_port:
                    valid_slice_count += 1

            if valid_slice_count == 0:
                ctx.error(
                    f"Service {service_name}: EndpointSlice has no address+port({expected_port}) pair"
                )
            else:
                ctx.ok(
                    f"Service {service_name}: EndpointSlice valid ({valid_slice_count} match)"
                )

    if configmap_data is None:
        ctx.error("missing ConfigMap data: hololive-common-config/hololive-bot-config")
        return ctx

    postgres_host = configmap_data.get("POSTGRES_HOST", "")
    postgres_port = configmap_data.get("POSTGRES_PORT", "")
    if postgres_host != "holo-postgres":
        ctx.error(
            f"POSTGRES_HOST must be 'holo-postgres', got '{postgres_host or '<empty>'}'"
        )
    else:
        ctx.ok("ConfigMap POSTGRES_HOST uses holo-postgres")

    if postgres_port != str(EXPECTED_PORTS["holo-postgres"]):
        ctx.error(
            f"POSTGRES_PORT must be '{EXPECTED_PORTS['holo-postgres']}', got '{postgres_port or '<empty>'}'"
        )
    else:
        ctx.ok("ConfigMap POSTGRES_PORT matches 5433")

    for key, (expected_host, expected_port) in URL_KEYS.items():
        raw = configmap_data.get(key)
        if raw is None:
            ctx.error(f"missing ConfigMap key: {key}")
            continue

        parsed = parse_url(raw, key, ctx)
        if parsed is None:
            continue

        host, port = parsed
        if host != expected_host:
            ctx.error(f"{key}: expected host={expected_host}, got host={host}")
        else:
            ctx.ok(f"{key}: host={host}")

        if port != expected_port:
            ctx.error(f"{key}: expected port={expected_port}, got port={port}")
        else:
            ctx.ok(f"{key}: port={port}")

    return ctx


def collect_resources(
    docs: list[dict[str, Any]],
) -> tuple[dict[str, dict[str, Any]], dict[str, list[dict[str, Any]]], dict[str, str] | None]:
    services: dict[str, dict[str, Any]] = {}
    endpoint_slices: dict[str, list[dict[str, Any]]] = {}
    configmap_data: dict[str, str] | None = None

    for doc in docs:
        kind = doc.get("kind")
        metadata = doc.get("metadata", {})
        namespace = metadata.get("namespace")
        name = metadata.get("name")

        if namespace != "hololive" or not isinstance(name, str):
            continue

        if kind == "Service" and name in EXPECTED_PORTS:
            services[name] = doc
            continue

        if kind == "EndpointSlice":
            labels = metadata.get("labels", {})
            service_name = labels.get("kubernetes.io/service-name")
            if service_name in EXPECTED_PORTS:
                endpoint_slices.setdefault(service_name, []).append(doc)
            continue

        if kind == "ConfigMap" and name in {"hololive-common-config", "hololive-bot-config"}:
            data = doc.get("data", {})
            if isinstance(data, dict):
                if configmap_data is None:
                    configmap_data = {}
                configmap_data.update({str(k): str(v) for k, v in data.items()})

    return services, endpoint_slices, configmap_data


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate hololive k8s external dependency routing consistency"
    )
    parser.add_argument(
        "--overlay",
        default="k8s/overlays/prod",
        help="kustomize overlay path (default: k8s/overlays/prod)",
    )
    parser.add_argument(
        "--mode",
        choices=["auto", "dns", "ip"],
        default="auto",
        help="routing mode (default: auto)",
    )
    args = parser.parse_args()

    repo_root = Path(__file__).resolve().parents[2]
    overlay = str((repo_root / args.overlay).resolve())

    try:
        rendered = run_kustomize(overlay)
    except Exception as err:  # noqa: BLE001
        print(f"[ERR] {err}")
        return 1

    docs = parse_yaml_documents(rendered)
    services, endpoint_slices, configmap_data = collect_resources(docs)
    mode = detect_mode(args.mode, services)

    print(f"[INFO] overlay={args.overlay}")
    print(f"[INFO] detected mode={mode}")

    ctx = validate(
        mode=mode,
        services=services,
        endpoint_slices=endpoint_slices,
        configmap_data=configmap_data,
    )

    for item in ctx.checks:
        print(f"[OK] {item}")

    if ctx.errors:
        for item in ctx.errors:
            print(f"[ERR] {item}")
        print(f"[FAIL] validation failed: {len(ctx.errors)} error(s)")
        return 1

    print("[PASS] external dependency validation succeeded")
    return 0


if __name__ == "__main__":
    sys.exit(main())
