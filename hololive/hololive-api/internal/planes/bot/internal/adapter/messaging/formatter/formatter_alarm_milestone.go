package formatter

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func isAllAlarmTypes(types domain.AlarmTypes) bool {
	return len(types) == 0 || len(types) == len(domain.AllAlarmTypes)
}

func (f *ResponseFormatter) formatAlarmTypesLabel(_ context.Context, types domain.AlarmTypes) string {
	if isAllAlarmTypes(types) {
		return f.messageStrings.Text(messagestrings.AlarmTypeAll)
	}

	names := make([]string, len(types))
	for i, t := range types {
		names[i], _ = f.messageStrings.Lookup(messagestrings.NamespaceAlarmType, t.String())
	}

	return strings.Join(names, "+")
}

type ambiguousMemberCandidate struct {
	Index int
	Name  string
}

type ambiguousMembersTemplateData struct {
	Prefix         string
	CommandExample string
	FirstName      string
	Candidates     []ambiguousMemberCandidate
}

func (f *ResponseFormatter) FormatAmbiguousMembers(ctx context.Context, candidates []*domain.Member, commandExample string) string {
	shaped := make([]ambiguousMemberCandidate, len(candidates))
	for i, m := range candidates {
		shaped[i] = ambiguousMemberCandidate{Index: i + 1, Name: fmt.Sprintf("%s (%s)", m.DisplayName(), m.GetOrg())}
	}

	firstName := ""

	if len(candidates) > 0 {
		firstName = candidates[0].QualifiedName()
	}

	data := ambiguousMembersTemplateData{
		Prefix:         f.Prefix(),
		CommandExample: commandExample,
		FirstName:      firstName,
		Candidates:     shaped,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAmbiguousMember, data)
	if err != nil {
		return f.renderFailureText()
	}

	return rendered
}
