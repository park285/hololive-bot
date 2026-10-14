package targetprojection

import (
	"crypto/sha256"
	"encoding/binary"
)

// reasonsCache는 guard 안에서 확인한 CURRENT header의 MVCC 버전에만 유효하다.
// 모든 refresher writer는 reasons와 header를 같은 transaction에서 갱신한다.
// generation만 비교하면 다른 writer의 같은 세대 reasons 변경을 놓치므로 버전도 확인한다.
// 이 값은 transaction의 commit 성공 후에만 Refresher에 공개한다.
type reasonsCache struct {
	generation int64
	version    string
	hash       [sha256.Size]byte
}

func (cache reasonsCache) matches(current currentProjection, hash [sha256.Size]byte) bool {
	return current.found && cache.generation == current.generation &&
		cache.version != "" && cache.version == current.version && cache.hash == hash
}

// normalize가 정렬·중복 제거한 전체 identity를 길이와 함께 hash하여 필드 경계도 보존한다.
func hashNormalizedReasons(reasons []TargetReason) [sha256.Size]byte {
	hash := sha256.New()
	var size [8]byte

	for _, reason := range reasons {
		for _, field := range [...]string{reason.SubjectKey, string(reason.ObservationKind), reason.ReasonKind, reason.ReasonKey} {
			binary.BigEndian.PutUint64(size[:], uint64(len(field)))
			_, _ = hash.Write(size[:])
			_, _ = hash.Write([]byte(field))
		}
	}

	var digest [sha256.Size]byte
	_ = hash.Sum(digest[:0])

	return digest
}
