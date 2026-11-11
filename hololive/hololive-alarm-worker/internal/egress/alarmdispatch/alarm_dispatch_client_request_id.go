package alarmdispatch

import (
	"errors"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 파생 ID로 보내면 재드레인 시 중복 제거가 깨지므로 발송을 거절한다.
var errAlarmDispatchSendUnitIdentityMissing = errors.New("alarm dispatch group has no persisted send-unit client_request_id")

var errAlarmDispatchSendUnitRoomMismatch = errors.New("alarm dispatch stored send unit room differs from the claimed group")

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
