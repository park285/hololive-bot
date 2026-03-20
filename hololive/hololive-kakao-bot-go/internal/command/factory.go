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

package command

// Factory: Command 생성 팩토리.
type Factory func(deps *Dependencies) Command

// DefaultFactories: 기본 내장 명령어 팩토리 목록.
func DefaultFactories() []Factory {
	return []Factory{
		func(deps *Dependencies) Command { return NewHelpCommand(deps) },
		func(deps *Dependencies) Command { return NewLiveCommand(deps) },
		func(deps *Dependencies) Command { return NewUpcomingCommand(deps) },
		func(deps *Dependencies) Command { return NewScheduleCommand(deps) },
		func(deps *Dependencies) Command { return NewAlarmCommand(deps) },
		func(deps *Dependencies) Command { return NewMemberInfoCommand(deps) },
		func(deps *Dependencies) Command { return NewSubscriberCommand(deps) },
		func(deps *Dependencies) Command { return NewStatsCommand(deps) },
	}
}

// NewMajorEventFactory: MajorEvent 명령어 팩토리를 생성한다.
func NewMajorEventFactory(repo MajorEventRepository) Factory {
	return func(deps *Dependencies) Command {
		return NewMajorEventCommand(deps, repo)
	}
}

// MemberNewsFactories: MemberNews 관련 명령어 팩토리 목록.
func MemberNewsFactories() []Factory {
	return []Factory{
		func(deps *Dependencies) Command { return NewMemberNewsCommand(deps) },
		func(deps *Dependencies) Command { return NewMemberNewsSubscriptionCommand(deps) },
	}
}

// BuildCommands: 주어진 팩토리 목록으로 Command 인스턴스를 생성한다.
func BuildCommands(deps *Dependencies, factories ...Factory) []Command {
	commands := make([]Command, 0, len(factories))
	for _, factory := range factories {
		if factory == nil {
			continue
		}

		command := factory(deps)
		if command == nil {
			continue
		}

		commands = append(commands, command)
	}

	return commands
}
