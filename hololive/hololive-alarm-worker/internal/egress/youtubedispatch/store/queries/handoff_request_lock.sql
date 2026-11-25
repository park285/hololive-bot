/* fragment:handoff_request_fence */

SELECT checked.token_matches, checked.request_matches, TRUE AS members_match, FALSE AS applied
FROM checked
