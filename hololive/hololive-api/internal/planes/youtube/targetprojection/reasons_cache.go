package targetprojection

import (
	"crypto/sha256"
	"encoding/binary"
)

type reasonsCache struct {
	generation int64
	version    string
	hash       [sha256.Size]byte
}

func (cache reasonsCache) matches(current currentProjection, hash [sha256.Size]byte) bool {
	return current.found && cache.generation == current.generation &&
		cache.version != "" && cache.version == current.version && cache.hash == hash
}

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
