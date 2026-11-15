package fonts

import _ "embed"

var (
	//go:embed Pretendard-Bold.ttf
	pretendardBoldData []byte

	//go:embed Pretendard-ExtraBold.ttf
	pretendardExtraBoldData []byte

	//go:embed YeonSung-Regular.ttf
	yeonSungData []byte

	//go:embed Jua-Regular.ttf
	juaData []byte

	//go:embed BlackHanSans-Regular.ttf
	blackHanSansData []byte

	//go:embed DoHyeon-Regular.ttf
	doHyeonData []byte

	//go:embed BagelFatOne-Regular.ttf
	bagelFatOneData []byte
)

// ScheduleCardData는 일정 카드 글꼴 파일 원본이다. 슬라이스는 embed 원본을 공유하므로 고치지 않는다.
type ScheduleCardData struct {
	PretendardBold      []byte
	PretendardExtraBold []byte
	YeonSung            []byte
	Jua                 []byte
	BlackHanSans        []byte
	DoHyeon             []byte
	BagelFatOne         []byte
	NotoSansJP          []byte
}

// ScheduleCard는 일정 카드가 쓰는 글꼴 원본을 돌려준다.
func ScheduleCard() ScheduleCardData {
	return ScheduleCardData{
		PretendardBold:      pretendardBoldData,
		PretendardExtraBold: pretendardExtraBoldData,
		YeonSung:            yeonSungData,
		Jua:                 juaData,
		BlackHanSans:        blackHanSansData,
		DoHyeon:             doHyeonData,
		BagelFatOne:         bagelFatOneData,
		NotoSansJP:          notoSansJPData,
	}
}
