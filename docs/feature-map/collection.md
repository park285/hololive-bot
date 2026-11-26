# 수집한 방송 상태가 조회와 알림에 반영되기까지

수집 AP가 모은 근거를 중앙 API가 적용해야 방송 조회와 알림의 원본이 바뀐다. AP가 직접 카카오 방에 보내지는 않는다.

## 하위 기능

채널·영상·커뮤니티·쇼츠·프로필·사진·일정·방송 상태 관측, job lease와 checkpoint, 부분 수집 지연, 관측 소비와 정본 반영, 종료 근거 확인, replay·보관 기한 정리. [수집 서비스](../current/services/collector.md)에 종류별 근거와 수명 규칙이 있다.

## 사용자 경로

- 직접 명령 없이 collector 스케줄러 → Holodex/공식 일정/YouTube.js → `source_observations` → API YouTube plane → 정본·알림 의도. [방송 목록](streams.md), [이력](history.md), [발송](dispatch.md)에서 결과를 관찰한다.
- YouTube.js 내부 `GET /health`, `POST /v1/bootstrap`, `/v1/community`, `/v1/content`, `/v1/channel`, `/v1/channel_live_check`, `/v1/video_live_check`. PO broker는 Unix 소켓에서 `GET /health`, `POST`·`DELETE /v1/session`, `POST /v1/challenge`, `/v1/activate`, `/v1/mint`를 제공한다. 사용자용 외부 API가 아니다.
- `source-observation-replay-epoch --activated-by … --reason …`은 이력 재처리의 불변 기준점을 등록한다. 이 명령을 공유 DB에 연결하지 않는다.

## Node 기록 테스트로 구동

```bash
(cd hololive/hololive-collector/youtubejs && node --test --test-concurrency=1 src/upstream-mapping.test.mjs)
```

`youtubejs/testdata` 및 `test-fixtures`와 함께 해석한 입력·정규화 결과를 본다. 임대 계약은 [CollectionJobLease의 코드 대응과 Mermaid 상태도](../../verification/tla/CollectionJobLease/COVERAGE.md#상태도)를 기준으로 분리해 확인한다.

```bash
TLC_WORKERS=2 bash scripts/ci/verify-tools.sh tlc verification/tla/CollectionJobLease/CollectionJobLease.tla
```

## 함정

일부 페이지만 읽은 결과는 영상 종료 증명이 아니다. 모델은 DB의 현재 소유권과 완료 슬롯 재개 금지를 검사하지, 공급자 호출 수·저장된 영상 내용·방 도착을 증명하지 않는다. 기본 드라이버에는 collector AP, PO 발급기나 외부 수집 응답 대역이 없으므로 fleet 전체의 발행을 검증한 것으로 보고하지 않는다.
