package acl

// 새 등록값(NewACLService seed·AddRoom)은 chatID만 받으므로 방 fixture는 signed i64 문자열이다.
const (
	testRoomA       = "1001"
	testRoomB       = "1002"
	testRoomX       = "1099"
	testBlockedRoom = "1003"

	testDBEnabledTrue  = "true"
	testDBEnabledFalse = "false"
)
