// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package messaging

type CommandParser interface {
	Parse(command string, args []string, raw string) (*ParsedCommand, bool)
}

type commandParserFunc func(command string, args []string, raw string) (*ParsedCommand, bool)

func (f commandParserFunc) Parse(command string, args []string, raw string) (*ParsedCommand, bool) {
	return f(command, args, raw)
}

func defaultCommandParsers(ma *MessageAdapter) []CommandParser {
	return []CommandParser{
		commandParserFunc(ma.tryLiveCommand),
		commandParserFunc(ma.tryUpcomingCommand),
		commandParserFunc(ma.tryScheduleCommand),
		commandParserFunc(ma.tryAlarmCommand),
		commandParserFunc(func(command string, _ []string, raw string) (*ParsedCommand, bool) {
			return ma.tryHelpCommand(command, raw)
		}),
		commandParserFunc(ma.trySubscriberCommand),
		commandParserFunc(ma.tryStatsCommand),
		commandParserFunc(ma.tryMemberInfoCommand),
		commandParserFunc(ma.tryMemberNewsSubscriptionCommand),
		commandParserFunc(ma.tryMemberNewsCommand),
		commandParserFunc(ma.tryMajorEventCommand),
	}
}
