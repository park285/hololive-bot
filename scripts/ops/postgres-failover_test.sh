#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/postgres-failover-test-fixture.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-controller.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-credentials.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-transition.sh"

for test_case in static_deployment_contracts_are_wired launcher_rejects_environment_injection systemd_pgpass_is_materialized_privately invalid_pgpass_credential_shapes_fail_closed \
  healthy_primary_records_fresh_observation standby_ahead_of_primary_fails_closed dry_run_never_fences_or_promotes \
  invalid_fence_ack_blocks_promotion stale_fence_ack_blocks_promotion primary_recovery_before_fence_cancels_failover writable_old_primary_after_fence_blocks_promotion fresh_fenced_standby_is_promoted_and_routed \
  route_failure_is_persisted_and_retried_without_repromotion stale_observation_blocks_promotion post_fence_clock_advance_blocks_promotion missing_route_hook_blocks_before_fencing untrusted_hook_parent_blocks_before_fencing \
  stale_health_signal_blocks_standby_controller unexpected_primary_without_marker_stays_unhealthy promotion_failure_on_standby_resumes_from_intent \
  ambiguous_promotion_timeout_reconciles_primary crash_after_promotion_restores_signal_and_route primary_fence_is_persistent_and_idempotent \
  primary_unfence_requires_reseeded_streaming_standby fence_and_unfence_share_transition_lock; do
  "${test_case}"
done
finish_postgres_failover_tests
