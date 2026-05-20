# Phase Index

## Phase 00: State Freeze And Evidence

목적: 현재 구현 상태를 다시 확인하고 작업 기준을 고정합니다.

파일: `phase-00-state-freeze-and-evidence.md`

출력: 코드 근거 목록, 현재 git state, 실행할 gate 목록

## Phase 01: Local Regression Gates

목적: active-active 관련 local tests와 compose render를 실행해 코드 기준 회귀를 확인합니다.

파일: `phase-01-local-regression-gates.md`

출력: command output 요약, 실패 원인 분류, 필요 최소 수정

## Phase 02: Osaka Smoke And Operational Evidence

목적: live Osaka AP 두 개가 active-active로 실제 동작하는지 read-only smoke와 운영 증거를 수집합니다.

파일: `phase-02-osaka-smoke-and-operational-evidence.md`

출력: `/ready`, health, metrics, logs, duplicate SQL evidence

## Phase 03: Proactive Valkey Readiness

목적: 첫 job claim 전에도 Valkey 장애를 `/ready`가 엄격히 반영할지 결정하고, 필요 시 구현합니다.

파일: `phase-03-proactive-valkey-readiness.md`

출력: code/test patch 또는 “reactive readiness 유지” 결정 기록

## Phase 04: Photo Sync Failover Policy

목적: PhotoSync를 AP-A 전용으로 유지할지, AP-B failover까지 허용할지 결정하고 문서/compose를 정렬합니다.

파일: `phase-04-photo-sync-failover-policy.md`

출력: policy decision, docs patch, optional compose patch

## Phase 05: Two-Scheduler Regression Test

목적: 같은 due job을 두 scheduler가 동시에 볼 때 Poll이 하나만 실행되는 regression test를 추가합니다.

파일: `phase-05-two-scheduler-regression-test.md`

출력: scheduler test patch, targeted test result

## Phase 06: Readiness Metrics And TTL Docs

목적: `/ready` recent counters 요구를 metrics-only로 대체할지 결정하고 lease TTL 정책을 runbook에 명시합니다.

파일: `phase-06-readiness-metrics-and-ttl-docs.md`

출력: docs patch 또는 readiness patch, tests
