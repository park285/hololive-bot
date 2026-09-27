package joblease

import (
	"errors"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

// acquire는 0144_08 RETURNING 식별자를 spec과 비교하므로, 네 식별자 중 하나라도 다르면 ErrInvalidJob이어야 한다.
func TestVerifyAcquireJobIdentityRejectsEachMismatchedField(t *testing.T) {
	t.Parallel()

	spec := communityJob()
	matching := acquiredJobIdentity{provider: string(spec.Provider), class: spec.Class, subjectKey: spec.SubjectKey}
	proof := contract.LeaseProof{JobKey: spec.JobKey, CollectionJobKind: spec.CollectionJobKind}

	if err := verifyAcquireJobIdentity(spec, &proof, matching); err != nil {
		t.Fatalf("matching identity: %v", err)
	}

	cases := map[string]func(*contract.LeaseProof, *acquiredJobIdentity){
		"provider": func(_ *contract.LeaseProof, identity *acquiredJobIdentity) {
			identity.provider = string(contract.ProviderHolodex)
		},
		"class": func(_ *contract.LeaseProof, identity *acquiredJobIdentity) { identity.class = "GLOBAL" },
		"kind": func(proof *contract.LeaseProof, _ *acquiredJobIdentity) {
			proof.CollectionJobKind = "channel_profile_collect"
		},
		"subject": func(_ *contract.LeaseProof, identity *acquiredJobIdentity) { identity.subjectKey = subjectChannelB },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mismatchedProof := proof
			mismatchedIdentity := matching
			mutate(&mismatchedProof, &mismatchedIdentity)

			if err := verifyAcquireJobIdentity(spec, &mismatchedProof, mismatchedIdentity); !errors.Is(err, ErrInvalidJob) {
				t.Fatalf("mismatched %s error = %v, want ErrInvalidJob", name, err)
			}
		})
	}
}
