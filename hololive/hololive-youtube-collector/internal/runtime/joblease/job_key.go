package joblease

import (
	"fmt"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// ExpectedLeaseSubject는 GLOBAL job의 고정 lease subject 또는 SUBJECT job의 동적 subject를 반환합니다.
func ExpectedLeaseSubject(job collection.JobContract, dynamicSubject string) (string, error) {
	if err := job.Validate(); err != nil {
		return "", fmt.Errorf("build collection job identity: %w: canonical job contract is invalid", ErrInvalidJob)
	}

	subject := dynamicSubject

	if job.Class() == collection.JobClassGlobal {
		subject = job.LeaseSubject()
	}

	if invalidBoundedToken(subject, 256) {
		return "", fmt.Errorf("build collection job identity: %w: subject is outside bounds", ErrInvalidJob)
	}

	return subject, nil
}

// BuildJobKey는 저장된 lease 행의 job_key 식별자를 만듭니다. GLOBAL job은 SQL과 같은 "global" 접미사를 씁니다.
func BuildJobKey(id collection.JobID, subject string) (string, error) {
	if !id.Valid() || invalidBoundedToken(subject, 256) {
		return "", fmt.Errorf("build collection job key: %w: identity is outside bounds", ErrInvalidJob)
	}

	definition, ok := canonicalJobContracts.Definition(id)
	if !ok {
		return "", fmt.Errorf("build collection job key: %w: canonical job contract is missing", ErrInvalidJob)
	}

	suffix := subject

	if definition.Class() == collection.JobClassGlobal {
		if subject != definition.LeaseSubject() {
			return "", fmt.Errorf("build collection job key: %w: global lease subject mismatch", ErrInvalidJob)
		}

		suffix = "global"
	}

	key := "collector:" + string(id.Provider) + ":" + string(id.Kind) + ":" + suffix
	if invalidBoundedToken(key, 512) {
		return "", fmt.Errorf("build collection job key: %w: key is outside bounds", ErrInvalidJob)
	}

	return key, nil
}
