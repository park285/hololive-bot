package collection

type MembershipScope struct {
	Kinds        []string
	ExactSubject bool
}

func MembershipScopeFor(job JobContract) MembershipScope {
	requested := job.RequestedKinds()
	kinds := make([]string, len(requested))

	for i, kind := range requested {
		kinds[i] = string(kind)
	}

	return MembershipScope{Kinds: kinds, ExactSubject: job.Membership() == JobMembershipExactSubject}
}
