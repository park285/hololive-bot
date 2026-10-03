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

package acl

import (
	"context"
	"fmt"
)

// 관리 plane과 봇 plane이 각각 Service 인스턴스를 들고 있어 한쪽의 변경이 다른 쪽 메모리에
// 닿지 않는다. Follow로 묶인 쪽이 이걸 호출해 DB 상태로 수렴한다.
// 읽기 전용이다 — 기본값을 새로 만들지 않으므로, 복제본이 관리 plane이 방금 쓴 DB 상태를
// 자기 기본값으로 덮어쓰지 않는다.
// PG 읽기부터 메모리 적용까지 mutation과 같은 reloadMu로 직렬화한다.
// 오래된 읽기가 같은 인스턴스의 최신 커밋을 메모리에서 되돌리지 않는다.
func (s *Service) Reload(ctx context.Context) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	enabled, mode, err := s.readSettingsFromDatabase(ctx)
	if err != nil {
		return fmt.Errorf("read settings from database: %w", err)
	}

	rooms, err := s.loadRoomsFromDatabase(ctx)
	if err != nil {
		return fmt.Errorf("load rooms from database: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = enabled
	s.mode = mode
	s.resetRoomMaps()
	s.populateRoomsFromRecords(rooms)

	return nil
}

func (s *Service) readSettingsFromDatabase(ctx context.Context) (enabled bool, mode ACLMode, err error) {
	s.mu.RLock()

	enabled = s.enabled
	mode = s.mode
	s.mu.RUnlock()

	enabledValue, enabledFound, err := s.store.GetSetting(ctx, dbKeyEnabled)
	if err != nil {
		return false, "", fmt.Errorf("reload ACL enabled setting: %w", err)
	}

	if enabledFound {
		parsed, parseErr := parseACLEnabledStrict(enabledValue)
		if parseErr != nil {
			return false, "", fmt.Errorf("reload ACL enabled setting: %w", parseErr)
		}

		enabled = parsed
	}

	modeValue, modeFound, err := s.store.GetSetting(ctx, dbKeyMode)
	if err != nil {
		return false, "", fmt.Errorf("reload ACL mode setting: %w", err)
	}

	if modeFound {
		parsed, parseErr := parseACLModeStrict(modeValue)
		if parseErr != nil {
			return false, "", fmt.Errorf("reload ACL mode setting: %w", parseErr)
		}

		mode = parsed
	}

	return enabled, mode, nil
}
