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
)

func validateBotDependencies(deps *Dependencies) error {
	if deps == nil {
		return errors.New("bot dependencies are required")
	}

	if deps.Logger == nil {
		return errors.New("logger dependency is required")
	}

	if deps.Client == nil {
		return errors.New("iris client dependency is required")
	}

	if deps.MessageAdapter == nil {
		return errors.New("message adapter dependency is required")
	}

	if deps.Formatter == nil {
		return errors.New("response formatter dependency is required")
	}

	if deps.Cache == nil {
		return errors.New("cache dependency is required")
	}

	if deps.Postgres == nil {
		return errors.New("postgres dependency is required")
	}

	if deps.Holodex == nil {
		return errors.New("holodex dependency is required")
	}

	if deps.Alarm == nil {
		return errors.New("alarm service dependency is required")
	}

	if deps.Matcher == nil {
		return errors.New("matcher dependency is required")
	}

	if deps.MembersData == nil {
		return errors.New("member data dependency is required")
	}

	return nil
}
