# Error Contract

## Current-compatible shape

```json
{
  "error": "room_id_required"
}
```

## Extended target shape

```json
{
  "error": "room_id_required",
  "message": "room_id is required",
  "request_id": "req_...",
  "details": {
    "field": "room_id"
  }
}
```

## Rules

- `error` is a machine-readable code.
- `message` is human-readable.
- Clients must not parse the full error string.
- Clients should branch by HTTP status and error code.
- New error codes must be added to the relevant contract package and contract document.

## Status mapping

| Status | Meaning | Example codes |
|---:|---|---|
| 400 | Invalid request | `invalid_request`, `room_id_required` |
| 404 | Domain resource missing | `no_subscribed_members`, `next_stream_not_found` |
| 409 | Conflict | `notification_in_progress` |
| 429 | Queue/rate limit | `queue_full`, `rate_limited` |
| 500 | Internal failure | `digest_generation_failed` |
| 503 | Dependency unavailable | `scheduler_unavailable` |
