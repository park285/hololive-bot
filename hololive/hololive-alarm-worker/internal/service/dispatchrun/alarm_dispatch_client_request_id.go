package dispatchrun

import (
	"errors"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 하나의 저장된 send-unit client_request_id로 모이지 않는 봉투 그룹을 나타낸다(errAlarmDispatchSendUnitIdentityMissing).
// Migration 141 이전 delivery(send_unit_id NULL)를 claim하던 legacy_head와 그룹 구성에서 ID를 파생하던 폴백을
// 지웠다(stack-audit 2026-09-26 T17, T18에서 활성 NULL 행 0건 확인). 이제 claim은 send unit이 있는 행만 읽고, 활성 NULL 행은
// migration 224의 alarm_dispatch_deliveries_active_send_unit_check가 쓰기 시점에 거절한다. 그래서 이 오류는 저장 ID가
// 비었거나 한 그룹에 섞이는 불변식 위반에서만 나온다. 파생 ID로 보내면 재드레인 때 admission 중복 제거에 접히지 않으므로
// 발송하지 않고 실패로 드러낸다.
var errAlarmDispatchSendUnitIdentityMissing = errors.New("alarm dispatch group has no persisted send-unit client_request_id")

// alarm dispatch는 send unit 전체를 한 메시지로 보내므로 저장된 send-unit client_request_id를 그대로 쓴다.
// ID 정본은 send unit을 만드는 hololive-shared dispatchoutbox(dispatch_group.go)이며, alarm-worker는 ID를 만들지 않는다.
// Karing chunk별 파생 ID는 DEC-20260926-hololive-karing-egress-disposition에 따라 Karing 경로와 함께 삭제했다.
func alarmDispatchClientRequestID(group alarmDispatchGroup) (string, error) {
	persisted := persistedAlarmDispatchClientRequestIDFromEnvelopes(group.envelopes)
	if persisted == "" {
		return "", errAlarmDispatchSendUnitIdentityMissing
	}

	return persisted, nil
}

func persistedAlarmDispatchClientRequestIDFromEnvelopes(envelopes []domain.AlarmQueueEnvelope) string {
	persisted := ""

	for i := range envelopes {
		candidate := strings.TrimSpace(envelopes[i].ClientRequestID)
		if candidate == "" || (persisted != "" && candidate != persisted) {
			return ""
		}

		persisted = candidate
	}

	return persisted
}
