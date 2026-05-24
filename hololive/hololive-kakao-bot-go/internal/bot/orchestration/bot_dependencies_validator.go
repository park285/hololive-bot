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

package orchestration

import (
	"errors"
	"log/slog"
)

func validateBotDependencies(deps *Dependencies) (streamRuntime, error) {
	if deps == nil {
		return nil, errors.New("bot dependencies are required")
	}

	core := deps.coreDeps()
	messaging := deps.messagingDeps()
	data := deps.dataDeps()
	stream := deps.streamDeps()

	if err := validateCoreDependencies(core); err != nil {
		return nil, err
	}
	core.logger.Info("Bot dependency snapshot", slog.Bool("stats_repo", stream.youTubeStatsRepository != nil))

	if err := validateMessagingDependencies(messaging); err != nil {
		return nil, err
	}
	if err := validateDataDependencies(data); err != nil {
		return nil, err
	}
	if err := validateStreamDependencies(stream); err != nil {
		return nil, err
	}

	return validateStreamRuntime(stream)
}

func validateCoreDependencies(core coreDependencies) error {
	if core.logger == nil {
		return errors.New("logger dependency is required")
	}
	return nil
}

func validateMessagingDependencies(messaging messagingDependencies) error {
	if messaging.client == nil {
		return errors.New("iris client dependency is required")
	}
	if messaging.messageAdapter == nil {
		return errors.New("message adapter dependency is required")
	}
	if messaging.formatter == nil {
		return errors.New("response formatter dependency is required")
	}
	return nil
}

func validateDataDependencies(data dataDependencies) error {
	if data.cache == nil {
		return errors.New("cache dependency is required")
	}
	if data.postgres == nil {
		return errors.New("postgres dependency is required")
	}
	return nil
}

func validateStreamDependencies(stream streamDependencies) error {
	if stream.holodex == nil {
		return errors.New("holodex dependency is required")
	}
	if stream.profiles == nil {
		return errors.New("profile service dependency is required")
	}
	if stream.alarm == nil {
		return errors.New("alarm service dependency is required")
	}
	if stream.matcher == nil {
		return errors.New("matcher dependency is required")
	}
	if stream.membersData == nil {
		return errors.New("member data dependency is required")
	}
	if stream.youTubeStatsRepository == nil {
		return errors.New("youtube stats repository dependency is required")
	}
	return nil
}

func validateStreamRuntime(stream streamDependencies) (streamRuntime, error) {
	holodexRuntime, ok := stream.holodex.(streamRuntime)
	if !ok {
		return nil, errors.New("holodex dependency does not implement stream runtime interface")
	}
	return holodexRuntime, nil
}
