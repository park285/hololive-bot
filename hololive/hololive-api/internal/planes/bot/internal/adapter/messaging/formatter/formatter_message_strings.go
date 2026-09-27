package formatter

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// RenderFailureMessageKey는 템플릿 렌더 실패 때 보내는 문구의 key다. 이 key는 messaging.ErrCommandProcessingFailed와
// 같은 행을 가리키며, 둘이 같다는 것은 messaging 패키지 테스트가 고정한다.
var RenderFailureMessageKey = messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: "command_processing_failed"}

// RequiredMessageStrings는 ResponseFormatter가 조회하는 message_strings 계약이다. 이 계약은 bot plane이 기동 때 검증한다.
// SendError가 쓰는 error namespace key(messaging.Err* 상수)는 호출자가 따로 더한다.
func RequiredMessageStrings() messagestrings.Requirements {
	timeFmtKeys := messagestrings.TimeFmtKeys()
	notifyKeys := messagestrings.NotifyKeys()
	keys := make([]messagestrings.Key, 0, 3+len(timeFmtKeys)+len(notifyKeys)+len(domain.AllAlarmTypes))

	keys = append(keys, RenderFailureMessageKey, messagestrings.MiscTimeUnknown, messagestrings.AlarmTypeAll)
	keys = append(keys, timeFmtKeys...)
	keys = append(keys, notifyKeys...)

	for _, alarmType := range domain.AllAlarmTypes {
		keys = append(keys, messagestrings.Key{Namespace: messagestrings.NamespaceAlarmType, Name: alarmType.String()})
	}

	return messagestrings.Requirements{
		Keys:       keys,
		Namespaces: []string{messagestrings.NamespaceOrg, messagestrings.NamespaceNewsCat, messagestrings.NamespaceAlarmType},
	}
}
