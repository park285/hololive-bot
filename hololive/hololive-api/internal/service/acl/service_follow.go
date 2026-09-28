package acl

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// ErrACLPropagation은 ACL 변경이 PG에는 이미 커밋됐지만 Follow로 묶인 인스턴스(봇 plane)가 PG에서
// 다시 읽지 못했음을 뜻한다. PG 쓰기를 되돌리지 않으므로 호출자는 이 오류를 "저장됐지만 봇 판정에는
// 아직 반영되지 않음"으로 다뤄야 한다. 같은 요청을 다시 보내면 PG 쓰기 없이 재동기화만 수행해 수렴한다.
var ErrACLPropagation = errors.New("ACL change committed to PostgreSQL but follower reload failed")

// Follow는 source의 mutation이 PG에 반영될 때마다 s가 PG에서 다시 읽게 묶는다.
// 관리 plane과 봇 plane은 한 프로세스 안에서 각자 Service를 들고 있어, 관리 plane의 변경이
// 봇 plane 판정 메모리에 닿으려면 통지가 필요하다. 같은 프로세스이므로 Pub/Sub 없이 source의
// mutation 경로에서 동기 호출한다 — 통지 자체는 유실되지 않지만 Reload(PG 읽기)는 실패할 수 있다.
// 실패하면 mutation이 ErrACLPropagation을 돌려주고, 봇 판정은 다음 성공한 Reload(같은 요청 재시도,
// 다음 변경, 재기동)까지 이전 스냅샷을 유지한다. 성공하면 mutation 응답 전에 봇 판정이 바뀐다.
// Runtime 조립 시(Start 전) 한 번 호출한다.
func (s *Service) Follow(source *Service) {
	if s == nil || source == nil || s == source {
		return
	}

	source.mu.Lock()
	defer source.mu.Unlock()

	source.changeListeners = append(source.changeListeners, s.reloadAfterChange)
}

// 요청 취소와 분리된 관리 요청 상한으로 Reload한다. Mutation은 이미 PG에
// 반영됐으므로 요청이 끊겨도 복제본은 수렴해야 한다.
func (s *Service) reloadAfterChange(ctx context.Context) error {
	reloadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.RequestTimeout.AdminRequest)
	defer cancel()

	return s.Reload(reloadCtx)
}

// notifyChange는 PG 반영과 메모리 갱신이 끝난 뒤(또는 no-op 재시도 경로에서), s.mu를 잡지 않은
// 상태에서 호출해야 한다. 모든 listener를 호출하고, 실패가 있으면 ErrACLPropagation으로 감싸 돌려준다.
func (s *Service) notifyChange(ctx context.Context) error {
	s.mu.RLock()

	listeners := s.changeListeners
	s.mu.RUnlock()

	var errs []error

	for _, listener := range listeners {
		if err := listener(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrACLPropagation, errors.Join(errs...))
}
