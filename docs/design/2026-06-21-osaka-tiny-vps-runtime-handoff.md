# Osaka Tiny VPS Runtime Handoff

> 실제 tailnet 주소/호스트는 private ops evidence 참조.

## Goal

Decide and implement the runtime mode for the two Osaka YouTube scraper VPS nodes:

- `osaka`: `<tailnet-osaka-a>`, hostname `<osaka-a-host>`, staged service `youtube-producer-a`, port `30005`
- `osaka2`: `<tailnet-osaka2-d>`, hostname `<osaka2-d-host>`, staged service `youtube-producer-d`, port `30035`

The repo-side AP topology is prepared, but no live deploy, restart, Docker install,
OpenBao Agent render, systemd mutation, or service start has been performed for
these nodes in this handoff.

## Fresh Evidence

Collected from `kapu` on 2026-06-21 KST with read-only SSH checks:

| Check | `osaka` | `osaka2` |
|---|---|---|
| SSH | `ubuntu` + `<ssh-key>` works | `ubuntu` + `<ssh-key>` works |
| Tailscale IP | `<tailnet-osaka-a>` | `<tailnet-osaka2-d>` |
| CPU/RAM | `2 vCPU`, `956MiB` RAM | `2 vCPU`, `956MiB` RAM |
| Swap | none | none |
| Disk | about `45G`, about `6%` used | about `45G`, about `6%` used |
| `tailscaled` | active | active |
| Docker | missing | missing |
| `sudo -n` | works | works |
| Central Postgres `<tailnet-central>:5433` | open | open |
| Central Valkey `<tailnet-central>:6379` | open | open |
| CLIProxy `<tailnet-central>:8787` | open | open |
| YouTube HTTPS | `200` | `200` |
| Holodex HTTPS | `200` | `200` |
| `/run/hololive-bot` | missing | missing |
| `/etc/hololive-bot` | missing | missing |
| `/opt/hololive-bot` | missing | missing |
| hololive/openbao/youtube systemd units | absent | absent |

## Repo Changes Prepared

- `scripts/deploy/ap-hosts/osaka.conf` now targets `<tailnet-osaka-a>`.
- `scripts/deploy/ap-hosts/osaka2.conf` stages `youtube-producer-d` on `<tailnet-osaka2-d>`.
- `deploy/compose/docker-compose.osaka.yml` metrics bind now uses `<tailnet-osaka-a>`.
- `deploy/compose/docker-compose.osaka2.yml` stages Docker Compose service `youtube-producer-d`.
- AP rsync, compose contract tests, H3 contract tests, project map, service docs, and runbook references are updated for `osaka2`.

## Decision Needed

Choose one runtime mode before live rollout.

### Option A: Docker Compose AP path

Pros:
- Matches current `ap-deploy.sh`, `ap-status.sh`, `ap-smoke.sh`, and rollback wrappers.
- Uses existing compose and image contract.
- Lowest repo automation work because `osaka` and `osaka2` configs are staged.

Cons:
- Docker is not installed on either VPS.
- `ap-deploy.sh` builds on the AP host; Go/Docker build on sub-1GB usable memory can OOM unless swap and build constraints are added.
- Leaves Docker daemon, build cache, and image lifecycle overhead on tiny scraper-only nodes.

Required follow-up:
- Install Docker and compose plugin on both nodes.
- Add swap before AP-side builds or change AP deploy to pull prebuilt images.
- Provision OpenBao Agent render for AP runtime files.
- Run `./scripts/deploy/ap-deploy.sh <host> --dry-run`, then explicit approved `--apply`.

### Option B: Host-native systemd AP path

Pros:
- Better fit for tiny scraper-only VPS nodes.
- Avoids Docker daemon and AP-side Docker build memory pressure.
- Central/build host can produce artifacts; AP receives only binaries and runtime data.

Cons:
- Current deploy/status/rollback automation is Docker Compose based.
- Needs a new repo-supported artifact deploy wrapper, systemd unit, status/smoke commands, and rollback path.
- Needs OpenBao Agent render path and service hardening to be implemented before live start.

Required follow-up:
- Add artifact build/sync path for `youtube-producer` and `healthcheck`.
- Add host-native env template and systemd unit.
- Add `ap-status`/`ap-smoke` or equivalent host-native checks.
- Add rollback via release symlink.
- Provision OpenBao Agent render for AP runtime files.

## Recommended Direction

Prefer Option B for long-term operation, because the nodes are scraper-only tiny
VPSes and currently have no Docker installed. If quick validation is more
important than runtime simplicity, use Option A only after adding swap and
accepting Docker install/build overhead.

## Live Work Gate

Before any live mutation, get explicit operator approval covering:

- target hosts: `osaka`, `osaka2`, or both
- runtime mode: Docker Compose or host-native `systemd`
- exact command class: install, render, deploy, restart, rollback
- expected impact: new scraper AP joins active-active lease namespace
- rollback path: stop new AP service/container first; existing `seoul`/main AP remains active

Keep raw secrets, rendered env values, private keys, and unfiltered logs out of notes.

## Validation Checklist For The Next Agent

- [ ] Reconfirm `git status --short` and preserve unrelated changes.
- [ ] Reconfirm SSH: `ubuntu@<tailnet-osaka-a>`, `ubuntu@<tailnet-osaka2-d>`.
- [ ] Reconfirm central port reachability from both nodes: `5433`, `6379`, `8787`.
- [ ] Decide Docker Compose vs host-native `systemd`.
- [ ] For Docker path, dry-run both AP wrappers before live apply.
- [ ] For host-native path, implement wrapper/unit/status checks before live start.
- [ ] Verify rendered files by metadata and env key names only.
- [ ] Verify `/health`, `/ready`, `mode=active-active`, `valkey_available=true`, `scraping_paused=false`.
- [ ] Verify logs after `change_started_at` for Postgres/Valkey success and absence of `ERR|panic|permission denied|x509|no such file`.
- [ ] Verify existing APs remain healthy if the new AP is stopped as rollback.

## Useful Commands

```bash
./scripts/deploy/ap-deploy.sh osaka --dry-run
./scripts/deploy/ap-deploy.sh osaka2 --dry-run
./scripts/logs/ap-status.sh osaka
./scripts/logs/ap-status.sh osaka2
./scripts/logs/ap-smoke.sh osaka
./scripts/logs/ap-smoke.sh osaka2
```

Live apply examples require separate approval:

```bash
I_APPROVE_OSAKA_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh osaka --apply
I_APPROVE_OSAKA2_ACTIVE_ACTIVE_DEPLOY=true ./scripts/deploy/ap-deploy.sh osaka2 --apply
```
