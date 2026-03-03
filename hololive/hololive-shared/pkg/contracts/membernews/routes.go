package membernews

const (
	// BasePath: internal membernews API route group
	BasePath = "/internal/membernews"
)

const (
	// SubscriptionsRoute: room subscription CRUD endpoint prefix.
	SubscriptionsRoute = "/subscriptions"
	// DigestRoute: generate room digest endpoint.
	DigestRoute = "/digest"
)

const (
	// SubscriptionsPath: absolute path for subscription collection.
	SubscriptionsPath = BasePath + SubscriptionsRoute
	// DigestPath: absolute path for room digest generation.
	DigestPath = BasePath + DigestRoute
)
