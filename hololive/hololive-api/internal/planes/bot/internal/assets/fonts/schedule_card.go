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

type ScheduleCardData struct {
	PretendardRegular   []byte
	PretendardBold      []byte
	PretendardExtraBold []byte
	YeonSung            []byte
	Jua                 []byte
	BlackHanSans        []byte
	DoHyeon             []byte
	BagelFatOne         []byte
	NotoSansJP          []byte
}

func ScheduleCard() ScheduleCardData {
	return ScheduleCardData{
		PretendardRegular:   pretendardRegularData,
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
