package collection

// MembershipScope는 acquire가 lease에 기록하는 job별 membership 범위다.
// Kinds는 cadence·roster kind의 정렬된 합집합이고(emission은 cadence의 부분집합),
// ExactSubject면 lease subject 하나, 아니면 해당 kind의 CURRENT 전체 target이 범위다.
// 범위 값은 lease 저장소와 발행 저장소가 함께 쓰고, 이 값으로 판정하는 SQL은 joblease가 소유한다.
type MembershipScope struct {
	Kinds        []string
	ExactSubject bool
}

// MembershipScopeFor는 컴파일된 job 계약에서 lease 범위를 만든다. 저장된 범위가 이 값과 다르면
// renew·snapshot·complete·publish가 모두 membership 무효로 거절한다.
func MembershipScopeFor(job JobContract) MembershipScope {
	requested := job.RequestedKinds()
	kinds := make([]string, len(requested))

	for i, kind := range requested {
		kinds[i] = string(kind)
	}

	return MembershipScope{Kinds: kinds, ExactSubject: job.Membership() == JobMembershipExactSubject}
}
