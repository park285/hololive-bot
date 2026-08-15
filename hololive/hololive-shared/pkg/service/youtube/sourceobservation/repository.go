package sourceobservation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

type PublishFenceVerifier interface {
	Verify(ctx context.Context, tx dbx.Tx, proof *contract.LeaseProof, observations []contract.Envelope) error
}

type JobContractSet interface {
	Allows(collectionJobKind string, provider contract.Provider, kind contract.ObservationKind) bool
	Definition(collectionJobKind string) (JobContract, bool)
}

type JobEmission struct {
	Provider contract.Provider
	Kind     contract.ObservationKind
}

type JobMembership string

const (
	JobMembershipExactSubject      JobMembership = "EXACT_SUBJECT"
	JobMembershipCurrentProjection JobMembership = "CURRENT_PROJECTION"
)

type JobContract struct {
	Class        string
	Membership   JobMembership
	FixedSubject string
	Emissions    []JobEmission
}

type StaticJobContracts map[string]JobContract

func (s StaticJobContracts) Allows(jobKind string, provider contract.Provider, kind contract.ObservationKind) bool {
	definition, ok := s[jobKind]
	if !ok {
		return false
	}
	for _, emission := range definition.Emissions {
		if emission.Provider == provider && emission.Kind == kind {
			return true
		}
	}
	return false
}

func (s StaticJobContracts) Definition(jobKind string) (JobContract, bool) {
	definition, ok := s[jobKind]
	return definition, ok
}

func InitialJobContracts() StaticJobContracts {
	return StaticJobContracts{
		"community_collect": {
			Class: "SUBJECT", Membership: JobMembershipExactSubject,
			Emissions: []JobEmission{{Provider: contract.ProviderYouTubeJS, Kind: contract.KindCommunityPage}},
		},
		"holodex_live": {
			Class: "GLOBAL", Membership: JobMembershipCurrentProjection,
			Emissions: []JobEmission{
				{Provider: contract.ProviderHolodex, Kind: contract.KindLiveSnapshot},
				{Provider: contract.ProviderHolodex, Kind: contract.KindViewerSample},
			},
		},
		"holodex_metadata": {
			Class: "GLOBAL", Membership: JobMembershipCurrentProjection,
			Emissions: []JobEmission{
				{Provider: contract.ProviderHolodex, Kind: contract.KindChannelStats},
				{Provider: contract.ProviderHolodex, Kind: contract.KindChannelPhoto},
			},
		},
		"holodex_schedule": {
			Class: "GLOBAL", Membership: JobMembershipCurrentProjection,
			Emissions: []JobEmission{
				{Provider: contract.ProviderHolodex, Kind: contract.KindSchedule},
			},
		},
		"official_schedule": {
			Class: "GLOBAL", Membership: JobMembershipExactSubject,
			FixedSubject: "global:hololive-schedule",
			Emissions:    []JobEmission{{Provider: contract.ProviderHololiveOfficial, Kind: contract.KindSchedule}},
		},
		"youtubejs_content": {
			Class: "SUBJECT", Membership: JobMembershipExactSubject,
			Emissions: []JobEmission{
				{Provider: contract.ProviderYouTubeJS, Kind: contract.KindVideoList},
				{Provider: contract.ProviderYouTubeJS, Kind: contract.KindShortsList},
			},
		},
		"youtubejs_channel_live": {
			Class: "SUBJECT", Membership: JobMembershipExactSubject,
			Emissions: []JobEmission{
				{Provider: contract.ProviderYouTubeJS, Kind: contract.KindLiveSnapshot},
			},
		},
		"youtubejs_channel_metadata": {
			Class: "SUBJECT", Membership: JobMembershipExactSubject,
			Emissions: []JobEmission{
				{Provider: contract.ProviderYouTubeJS, Kind: contract.KindChannelStats},
				{Provider: contract.ProviderYouTubeJS, Kind: contract.KindChannelProfile},
				{Provider: contract.ProviderYouTubeJS, Kind: contract.KindChannelPhoto},
			},
		},
		"youtubejs_viewer": {
			Class: "SUBJECT", Membership: JobMembershipExactSubject,
			Emissions: []JobEmission{{Provider: contract.ProviderYouTubeJS, Kind: contract.KindViewerSample}},
		},
	}
}

type Repository struct {
	pool          *pgxpool.Pool
	supported     SupportedContractSet
	jobContracts  JobContractSet
	fenceVerifier PublishFenceVerifier
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return NewRepositoryWithContracts(pool, InitialSupportedContracts(), InitialJobContracts(), nil)
}

func NewRepositoryWithContracts(
	pool *pgxpool.Pool,
	supported SupportedContractSet,
	jobContracts JobContractSet,
	fenceVerifier PublishFenceVerifier,
) *Repository {
	repository := &Repository{
		pool: pool, supported: supported, jobContracts: jobContracts, fenceVerifier: fenceVerifier,
	}
	if repository.fenceVerifier == nil {
		repository.fenceVerifier = sqlPublishFenceVerifier{jobs: jobContracts}
	}
	return repository
}

func (r *Repository) validate() error {
	if r == nil || r.pool == nil || r.supported == nil || r.jobContracts == nil || r.fenceVerifier == nil {
		return fmt.Errorf("validate source observation repository: %w", ErrInvalidRepository)
	}
	return nil
}
