#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/postgres-failover-test-fixture.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-controller.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-credentials.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-transition.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-primary.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-unfence.sh"
source "${SCRIPT_DIR}/lib/postgres-failover-test-cases-hook-ownership.sh"

for test_case in static_deployment_contracts_are_wired launcher_rejects_environment_injection systemd_pgpass_is_materialized_privately invalid_pgpass_credential_shapes_fail_closed route_credentials_are_required_and_private route_environment_paths_are_not_injected \
  healthy_primary_records_fresh_observation standby_ahead_of_primary_fails_closed dry_run_never_fences_or_promotes \
  invalid_fence_ack_blocks_promotion stale_fence_ack_blocks_promotion primary_recovery_before_fence_cancels_failover writable_old_primary_after_fence_blocks_promotion fresh_fenced_standby_is_promoted_and_routed \
  route_failure_is_persisted_and_retried_without_repromotion writable_old_primary_blocks_route_retry stale_observation_blocks_promotion post_fence_clock_advance_blocks_promotion missing_route_hook_blocks_before_fencing untrusted_hook_parent_blocks_before_fencing non_root_fence_hook_blocks_before_fencing non_root_route_hook_blocks_before_fencing non_root_route_hook_blocks_intent_recovery non_root_hook_parent_blocks_in_production non_root_production_client_paths_remain_allowed \
  stale_health_signal_blocks_standby_controller unexpected_primary_without_marker_stays_unhealthy promotion_failure_on_standby_resumes_from_intent \
  ambiguous_promotion_timeout_reconciles_primary crash_after_promotion_restores_signal_and_route primary_fence_is_persistent_and_idempotent \
  primary_fence_drain_failure_blocks_durable_fence primary_fence_restores_route_after_incomplete_fence primary_fence_crash_points_preserve_intent primary_fence_rolls_back_post_intent_failure primary_fence_reload_guard_blocks_before_drain \
  primary_unfence_requires_reseeded_streaming_standby fence_and_unfence_share_transition_lock; do
  "${test_case}"
done
finish_postgres_failover_tests
bash "${SCRIPT_DIR}/postgres-failover-route_test.sh"
bash "${SCRIPT_DIR}/postgres-failover-ssh-dispatch_test.sh"
if [[ "$(/usr/bin/id -u)" != 0 ]] && sudo -n true >/dev/null 2>&1; then
  sudo -n bash "${SCRIPT_DIR}/postgres-failover-route_test.sh"
fi
