package formatter

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

var RenderFailureMessageKey = messagestrings.Key{Namespace: messagestrings.NamespaceError, Name: "command_processing_failed"}

func RequiredMessageStrings() messagestrings.Requirements {
	timeFmtKeys := messagestrings.TimeFmtKeys()
	notifyKeys := messagestrings.NotifyKeys()
	keys := make([]messagestrings.Key, 0, 4+len(timeFmtKeys)+len(notifyKeys)+len(domain.AllAlarmTypes))

	keys = append(keys, RenderFailureMessageKey, messagestrings.MiscSchedulePosterNotFound, messagestrings.MiscTimeUnknown, messagestrings.AlarmTypeAll)
	keys = append(keys, timeFmtKeys...)
	keys = append(keys, notifyKeys...)

	for _, alarmType := range domain.AllAlarmTypes {
		keys = append(keys, messagestrings.Key{Namespace: messagestrings.NamespaceAlarmType, Name: alarmType.String()})
	}

	return messagestrings.Requirements{
		Keys:       keys,
		Namespaces: []string{messagestrings.NamespaceOrg, messagestrings.NamespaceAlarmType},
	}
}

func (f *ResponseFormatter) SchedulePosterNotFound() string {
	return f.messageStrings.Text(messagestrings.MiscSchedulePosterNotFound)
}
