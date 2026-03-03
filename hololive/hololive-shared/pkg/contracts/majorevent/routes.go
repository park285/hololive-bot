package majorevent

const (
	// BasePath: internal majorevent API route group
	BasePath = "/internal/majorevent"
)

const (
	// SubscriptionsRoute: room subscription CRUD endpoint prefix.
	SubscriptionsRoute = "/subscriptions"
)

const (
	// SubscriptionsPath: absolute path for subscription collection.
	SubscriptionsPath = BasePath + SubscriptionsRoute
)
