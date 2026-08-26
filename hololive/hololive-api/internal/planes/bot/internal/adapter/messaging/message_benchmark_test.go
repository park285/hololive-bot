package messaging

import (
	"testing"

	"github.com/park285/iris-client-go/v2/webhook"
)

var benchmarkParsedCommandSink *ParsedCommand

func BenchmarkParseMessage(b *testing.B) {
	adapter := NewMessageAdapter("!", "")

	for _, benchmark := range []struct {
		name    string
		message string
	}{
		{name: "first_parser_no_args", message: "!라이브"},
		{name: "middle_parser_no_args", message: "!도움"},
		{name: "last_parser_no_args", message: "!달력"},
		{name: "unknown_no_args", message: "!없는명령"},
		{name: "known_multiple_args", message: "!예정 30 페코라"},
		{name: "unknown_multiple_args", message: "!없는명령 하나 둘 셋 넷 다섯"},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			message := &webhook.Message{Msg: benchmark.message}

			b.ReportAllocs()

			for b.Loop() {
				benchmarkParsedCommandSink = adapter.ParseMessage(message)
			}
		})
	}
}
