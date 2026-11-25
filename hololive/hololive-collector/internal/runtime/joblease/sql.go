package joblease

import (
	"embed"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*
var sqlAssets embed.FS

var mustSQL = sqlassets.MustStatementReader(sqlAssets, "queries")

var (
	subjectCandidatesForProjectionSQL   = mustSQL("subject_candidates_for_projection.sql")
	globalCandidatesForProjectionSQL    = mustSQL("global_candidates_for_projection.sql")
	repositoryLeaseAcquireSQL           = mustSQL("repository_lease_acquire.sql.tpl").With("{{bundle}}", mustSQL("repository_target_bundle_kinds.sql"))
	repositoryLeaseAcquireExactSQL      = mustSQL("repository_lease_acquire.sql.tpl").With("{{bundle}}", mustSQL("repository_target_bundle_exact.sql"))
	repositoryLeaseInitializeSQL        = mustSQL("repository_lease_initialize.sql")
	completeCurrentLeaseSQL             = mustSQL("complete_current_lease.sql")
	completeCurrentLeaseWithErrorSQL    = mustSQL("complete_current_lease_with_error.sql")
	deferSQL                            = mustSQL("defer.sql")
	leaseMembershipQuerySQL             = mustSQL("lease_membership_query.sql")
	lockActiveLeaseSQL                  = mustSQL("lock_active_lease.sql")
	repositoryLeaseReleaseSupersededSQL = mustSQL("repository_lease_release_superseded.sql")
	releaseSQL                          = mustSQL("release.sql")
	renewTxSQL                          = mustSQL("renew_tx.sql")
	currentProjectionGenerationSQL      = mustSQL("current_projection_generation.sql")
	lockProjectionGuardShareSQL         = mustSQL("lock_projection_guard_share.sql")
	loadExactSQL                        = mustSQL("load_exact.sql")
	loadProjectionSQL                   = mustSQL("load_projection.sql")
)
