AP 상태머신 이관은 main 병합과 운영 적용까지 끝났다. 다음 세션은 acceptance 관측, 보존 자료 정리 승인, 리뷰 잔여 후속만 다룬다. 끝난 리뷰·build·적용은 반복하지 않는다.

## 먼저 읽을 것

- /home/kapu/work/iris-stack/hololive-bot/docs/current/plans/2026-10-19-ap-state-machine-consolidation-handoff.md — §4.6(9차 결과)과 §5가 정본이다.
- 운영 조회는 hololive-bot-ops(inspection), DB 조회는 stack-platform-ops(postgres) 지침을 적용한다.

## 마지막 확인 상태 — 새 세션에서 최신 상태 확인 필요

- main: `8f5dec487e5415734ac5bbf92e6788cc5aced13c`(#667). 트리는 로컬 검증한 `4b5df2481`과 같다.
- 운영 API: image `sha256:042f2ee03eee8e7c498d4fd447f00d888da36bf8471896a1a7ac280cf9005b83`, revision `8f5dec487…`, StartedAt 2026-10-20T06:23:38.953Z.
- 운영 alarm-worker: `4a1a8eabd` image 그대로(소스 동일).
- DB ledger 200(328 포함). 06:34:40Z까지 RR 읽기 전용 bundle `0|0|0|0`, live `0|0|0`.
- 복구점: `/opt/hololive-bot/compose/current/backups/apsm-8f5dec487-20261020T062226Z/`와 `artifacts/worker-profile/releases/apsm-8f5dec487-20261020T062226Z/hololive-api/{previous,candidate}`. 328 뒤에는 전진 복구만 가능하다.
- 운영 `/var/tmp/apsm-8f5dec487`에 전송 image·묶음·스크립트가 남아 있다.

## 할 일

1. 운영 API revision·image·StartedAt·재시작, ledger, live drift를 읽기 전용으로 확인한다. drift가 0이 아니면 그 원인부터 조사한다.
2. acceptance를 판단할 수 있으면 사용자에게 알린다. 보존 자료(이전 apsm 릴리스의 복구점·retained image tag·`/var/tmp` 전송 파일) 정리는 별도 승인을 받은 뒤에만 한다.
3. 리뷰 잔여(§4.6·§5) 중 사용자가 고른 항목만 새 worktree에서 고친다. 운영 적용이 끝난 migration 파일(325–339)은 checksum 때문에 수정하지 않는다.

## 경계와 전달

- 운영 시험 관측 삽입, 실제 메시지 발송, 수집 범위 변경을 하지 않는다. 기존 자격 증명은 승인된 인증에만 쓰며 값·raw payload를 출력하지 않는다.
- 끝난 승인(328 적용까지)은 보존 자료 정리, AP·인프라·secret 변경, 새 DB/secret 백업, Git tag를 포함하지 않는다.
- 메인 체크아웃과 다른 작업의 worktree를 수정·정리하지 않는다. Iris/ 루트·Iris/native/**·Iris/tools/** 스캔 금지를 유지한다.
- 보고는 한국어로 하며 확인된 사실·추론·미확인을 구분한다.
