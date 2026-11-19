package delivery

type SendResult struct {
	Attempted   int
	Sent        int
	Skipped     int
	Failed      int
	FailedRooms []string
}
