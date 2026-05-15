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

## Historical Or Plan-Kit Candidates

- `docs/holobot-pg-valkey-hybrid-hardening-plan-v4/`
- `docs/holobot-valkey-plan/`
- `docs/hololive-bot-baseline-bigbang-llm-docs-v8/`
- `docs/hololive-bot-integrated-refactor-v3/`
- `docs/hololive-main-server-logs-mirror-v2/`
- `docs/hololive_scraper_plan_v2/`

These directories must not be moved until links, active references, and current operational relevance are checked.

## Move Rule

Move one directory family at a time. Each move must include link updates, bridge files when needed, and architecture doc checks.
