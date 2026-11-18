package youtubedispatch

type DispatchResult struct {
	SuccessDeliveryIDs []int64
	TouchedOutboxIDs   []int64
	FailedDeliveries   int
}
