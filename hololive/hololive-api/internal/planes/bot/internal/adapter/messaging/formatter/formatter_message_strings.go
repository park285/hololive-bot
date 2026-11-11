package formatter

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// RenderFailureMessageKey는 템플릿 렌더 실패 때 보내는 문구의 key다. 이 key는 messaging.ErrCommandProcessingFailed와
// 같은 행을 가리키며, 둘이 같다는 것은 messaging 패키지 테스트가 고정한다.
var RenderFailureMessageKey = messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: "command_processing_failed"}

// RequiredMessageStrings는 bot plane이 기동 때 검증하는 ResponseFormatter의 message_strings 계약이며, SendError의 error key(messaging.Err*)는 호출자가 더한다.
// NotifyMemberNews* key는 formatter가 더 읽지 않지만 key와 seed를 함께 지우는 migration 전까지 검증 대상에 남는다.
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
