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

package info

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *MemberInfoCommand) memberGroups(member *domain.Member) []string {
	if member == nil {
		return nil
	}

	if len(member.Units) != 0 {
		return member.Units
	}

	if group, ok := orgDirectoryGroups[member.Org]; ok {
		return []string{group}
	}

	if member.Org != "" {
		return []string{member.Org + " (기수 미등록)"}
	}

	return nil
}

// PrimaryMemberName은 등록된 한국어 이름을 우선하고 없으면 원래 이름을 표시한다.
func PrimaryMemberName(member *domain.Member) string {
	if member == nil {
		return ""
	}

	if name := strings.TrimSpace(member.NameKo); name != "" {
		return name
	}

	return member.Name
}
