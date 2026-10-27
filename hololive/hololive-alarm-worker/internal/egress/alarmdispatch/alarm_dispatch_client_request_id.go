package alarmdispatch

import (
	"errors"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// errAlarmDispatchSendUnitIdentityMissing은 저장 ID가 없거나 한 그룹에 섞인 상태다.
// 파생 ID로 보내면 재드레인 시 중복 제거가 깨지므로 발송을 거절한다.
var errAlarmDispatchSendUnitIdentityMissing = errors.New("alarm dispatch group has no persisted send-unit client_request_id")

// errAlarmDispatchSendUnitRoomMismatch는 저장된 send unit 요청의 room이 claim한 group과 다른 상태다.
var errAlarmDispatchSendUnitRoomMismatch = errors.New("alarm dispatch stored send unit room differs from the claimed group")

// send unit을 한 메시지로 보내므로 생산자가 저장한 client_request_id를 그대로 쓴다.
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
