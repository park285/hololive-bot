# Upstream 계약 대조에서 확인한 알람 정정

2026-09-09. `DEC-20260909-hololive-admin-bigbang-replacement`의 upstream 보존 원칙과 `DEC-20260906-hololive-unit-b-member-subscriptions`(accepted/verified)에 따라, BFF 명세·화면을 실제 방 구독 계약에 맞춥니다. 변경 대상은 동시 교체하는 관리자 BFF와 프런트이며 upstream API·업무 DB를 변경하지 않습니다.

`hololive/hololive-shared/pkg/domain/alarm_interfaces.go`의 AlarmEntry는 roomId/roomName/channelId/memberName 네 필드만 반환합니다. `api_alarm.go`의 DeleteAlarm은 roomId/channelId를 읽고 userId를 읽지 않으며 removed를 반환합니다. 이 소스들은 미커밋 변경이 없으며, domain 변경의 최근 커밋은 `6c026e173 feat(alarm): UNIT B 멤버별 방 구독과 감사 수정 반영 (#472)`입니다.

기존 관리자 명세의 Alarm.userId/userName과 DeleteAlarmRequest.userId, 화면의 사용자 그룹·이름 편집은 이 소스와 일치하지 않습니다. 특히 사용자 이름 정렬·검색은 실제 응답에서 undefined 값을 사용합니다. 가짜 사용자 값이나 호환 shadow 필드를 만들지 않습니다. Alarm은 네 필드로, 삭제 입력은 roomId/channelId로 정정하고, 화면은 방으로 그룹화하며 삭제 확인에 해당 방·채널 구독의 범위를 표시합니다. 구형 bundle과의 혼합은 generation fence가 거부해야 합니다.

기존 `/admin/api/holo/names/user` 등록 API는 별도 계약으로 남기고 SDK·typed adapter·호출 fixture로 검증합니다. 알람 목록에서 사용자 ID가 제공된다는 가정이나 사용자별 알람 편집이 지원된다는 주장은 제거합니다. T01 baseline manifest는 당시 BFF의 스냅샷으로 유지하며 후보의 정정을 별도 항목으로 기록합니다. 지원되지 않는 기존 UI 가정을 기능 검증 PASS로 세지 않습니다.

이 문서는 로컬 구현 범위 안에서 확인한 계약 정합화 근거입니다. 실제 운영 전환·배포·데이터 변경 승인이나 최종 기능 검증 결과가 아닙니다.

## C03 · ACL 응답 enum과 후보 화면 상태

T07에서 `adapters/holo/reads.go`의 `roomsResponse.valid()`와 `mutations.go`의 `ACLResponse.valid()`가 이미 whitelist/blacklist 두 값만 수용함을 확인했습니다. 기존 정본의 두 응답 field는 넓은 string이어서 프런트가 타입 단언과 임의 기본값을 사용했습니다. `RoomsResponse.aclMode`와 `SetAclResponse.mode`를 실제 소유 응답과 같은 enum으로 선언했습니다. 입력의 대소문자·공백 정규화 계약은 보존합니다. 이 보정 뒤 생성 세대는 `bde388dc44b6614a5902dadb46e716aecc37f4acd5405dae51687b1ce62cad5f`입니다.

설정 GET은 Settings 객체와 alarmAdvanceMinutes 0~1440을 필수로 반환합니다. empty 객체는 정상 빈 결과가 아니며, 기존 화면에도 변경 확인 modal이 없습니다. T01의 공통 feature 상태 목록에 포함된 settings의 empty/confirmation_cancel은 후보에서 지원한다고 꾸미지 않습니다. 정상 0값과 잘못된 응답 거부, 편집 1~60·초안 보존을 실제 검증 대상으로 연결합니다. 다른 메뉴의 실제 빈 목록·위험 확인 취소는 각각 검증합니다.
