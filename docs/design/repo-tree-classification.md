# Repository Tree Classification

## Purpose

This document classifies existing top-level `docs/` directories before any move. It is a staging document, not current operational SSOT.

## Current

- `docs/current/` - current operational SSOT
- `docs/architecture/` - architecture gate assets and thresholds retained for compatibility with existing scripts
- `docs/runbook_execution/` - deployment/release runbook location retained until each current runbook bridge is verified

## Design And Plans

- `docs/design/` - design proposals
- `docs/superpowers/specs/` - agent-driven design specs
- `docs/superpowers/plans/` - executable plans

## Historical Plan Kits

- `docs/history/plan-kits/holobot-pg-valkey-hybrid-hardening-plan-v4/`
- `docs/history/plan-kits/holobot-valkey-plan/`
- `docs/history/plan-kits/hololive-bot-baseline-bigbang-llm-docs-v8/`
- `docs/history/plan-kits/hololive-bot-integrated-refactor-v3/`
- `docs/history/plan-kits/hololive-main-server-logs-mirror-v2/`
- `docs/history/plan-kits/hololive_scraper_plan_v2/`

These directories were moved out of the top-level `docs/` entrypoint after checking active references and current operational relevance. They remain historical implementation records, not current SSOT.

## Move Rule

Future plan-kit moves must happen one directory family at a time. Each move must include link updates, bridge files when needed, and architecture doc checks.
