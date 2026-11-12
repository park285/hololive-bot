package scheduleposter

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 츠즈리 자료의 broken 판독은 운영에서 시각·VALORANT가 빠지고 일본어가 남은 채 게시된 실제 번역본이다.
const (
	tsuzuriOriginal = `今週の
おてがみ
Thank you!
@shusuchii
ゲーム
FPS
雑談
歌枠
お休み
コラボ
メンバー限定
配信スケジュール
Weekly Schedule
장식: MAIL
장식: POST
MON
09
21:00〜
OVERWATCH @キョーコ
TUE
10
18:00〜VALORANT
5
@せんぱいコラボ
WED
11
20:00〜コラボ(自枠ない)
←たぶん
THU
12
or
21:00〜VALORANT
ソロコンペ
or 「時のオカリナ」
FRI
13
18:00〜うた枠
SAT
14
19:00〜「一致するまで終われまテン」
@アソビ★まわり隊 めらch.
21:30〜「時のオカリナ」
SUN
15
or
or
21:00〜ざつだん
or
VALORANT
休みになる
かのうせい大
予定は変更になることもあります！最新の情報はXをチェックしてね！`
	tsuzuriTable = `今週のおてがみ ⇒ 이번 주의 편지
Thank you! ⇒ Thank you!
@shusuchii ⇒ @shusuchii
ゲーム ⇒ 게임
FPS ⇒ FPS
雑談 ⇒ 잡담
歌枠 ⇒ 노래 방송
お休み ⇒ 휴방
コラボ ⇒ 콜라보
メンバー限定 ⇒ 멤버십 한정
配信スケジュール ⇒ 방송 스케줄
Weekly Schedule ⇒ Weekly Schedule
MON ⇒ 월
09 ⇒ 09
21:00〜 OVERWATCH @キョーコ ⇒ 21:00~ OVERWATCH @햐쿠토 쿄코
TUE ⇒ 화
10 ⇒ 10
18:00〜VALORANT ⇒ 18:00~ VALORANT
5 ⇒ 5
@せんぱいコラボ ⇒ @선배 콜라보
WED ⇒ 수
11 ⇒ 11
20:00〜コラボ(自枠ない) ⇒ 20:00~ 콜라보(개인 방송 없음)
←たぶん ⇒ ←아마도
THU ⇒ 목
12 ⇒ 12
21:00〜VALORANT ソロコンペ or 「時のオカリナ」 ⇒ 21:00~ VALORANT 솔로 경쟁전 or 「시간의 오카리나」
FRI ⇒ 금
13 ⇒ 13
18:00〜うた枠 ⇒ 18:00~ 노래 방송
SAT ⇒ 토
14 ⇒ 14
19:00〜「一致するまで終われまテン」 ⇒ 19:00~ 「일치할 때까지 끝낼 수 없텐」
@アソビ★まわり隊 めらch. ⇒ @아소비★마와리타이! 아치치 메라 ch.
21:30〜「時のオカリナ」 ⇒ 21:30~ 「시간의 오카리나」
SUN ⇒ 일
15 ⇒ 15
21:00〜ざつだん or VALORANT ⇒ 21:00~ 잡담 or VALORANT
休みになるかのうせい大 ⇒ 휴방이 될 가능성 큼
予定は変更になることもあります！最新の情報はXをチェックしてね！ ⇒ 일정은 변경될 수도 있습니다! 최신 정보는 X를 확인해 줘!`
	tsuzuriBrokenRender = `이번 주의
편지
Thank you!
@shusuchii
게임
FPS
잡담
노래 방송
휴방
콜라보
멤버십 한정
방송 스케줄
MAIL
POST
월
09
21:00 ~
OVERWATCH @햐쿠토 쿄코
화
10
18:00 ~ VALORANT
@선배 콜라보
수
11
←아마도
콜라보(개인 방송 없음)
THU
12
솔로 경쟁전 or 「시간의 오카리나」
금
13
노래 방송
SAT
14
「일치할 때까지 끝낼 수 없텐」
@아소비★まわり隊 아치치 메라 ch.
「시간의 오카리나」
SUN
15
or
or
ざつだん
휴방이 될
가능성 큼
일정은 변경될 수도 있습니다! 최신 정보는 X를 확인해 줘!`
	tsuzuriGoodRender = `이번 주의
편지
Thank you!
@shusuchii
게임
FPS
잡담
노래 방송
휴방
콜라보
멤버십 한정
방송 스케줄
Weekly Schedule
월
09
21:00〜
OVERWATCH @햐쿠토 쿄코
화
10
18:00 ~ VALORANT
⑤
@선배 콜라보
수
11
20:00～콜라보(개인 방송 없음)
←아마도
목
12
21:00~VALORANT
솔로 경쟁전 or 「시간의 오카리나」
금
13
18:00 ~ 노래 방송
토
14
19:00~「일치할 때까지 끝낼 수 없텐」
@아소비★마와리타이! 아치치 메라 ch.
21：30~「시간의 오카리나」
일
15
21:00 ~ 잡담
or
VALORANT
휴방이 될
가능성 큼
일정은 변경될 수도 있습니다! 최신 정보는 X를 확인해 줘!`
)

// 소피아 자료의 render 판독은 운영에 게시된 번역본으로, 원본의 시각·이름을 모두 옮긴 통과 사례다.
const (
	sophiaOriginal = `Weekly
Schedule
11/9〜11/15
予定は変更になる
可能性があります
MON
19:00〜 理系女子あるあるコラボ
@博衣こより先輩ch.
TUE
20:00〜 ホロドリ配信
WED
20:00〜 コラボ配信
THU
22:00〜 酒を呑みながら語り合う
@儒鳥風亭らでんch.
FRI
22:00〜 コラボ配信
SAT
20:00〜 一致するまで終われまテン!!
with アソビ★まわり隊! @熱千めらch.
SUN
23:00〜 メン限配信
illustration
ふろーる 様 @flowle_ow0
Sorashina Sopia
宙科そぴあ
Sorashina Sopia`
	sophiaTable = `Weekly Schedule ⇒ Weekly Schedule
11/9〜11/15 ⇒ 11/9~11/15
予定は変更になる可能性があります ⇒ 일정은 변경될 가능성이 있습니다
MON ⇒ 월
19:00〜 理系女子あるあるコラボ ⇒ 19:00~ 이과 여자 공감 콜라보
@博衣こより先輩ch. ⇒ @하쿠이 코요리 선배 ch.
TUE ⇒ 화
20:00〜 ホロドリ配信 ⇒ 20:00~ 홀로도리 방송
WED ⇒ 수
20:00〜 コラボ配信 ⇒ 20:00~ 콜라보 방송
THU ⇒ 목
22:00〜 酒を呑みながら語り合う ⇒ 22:00~ 술을 마시며 이야기 나누기
@儒鳥風亭らでんch. ⇒ @주후테이 라덴 ch.
FRI ⇒ 금
22:00〜 コラボ配信 ⇒ 22:00~ 콜라보 방송
SAT ⇒ 토
20:00〜 一致するまで終われまテン!! ⇒ 20:00~ 일치할 때까지 끝낼 수 없습니다!!
with アソビ★まわり隊! @熱千めらch. ⇒ with 아소비★마와리타이! @아치치 메라 ch.
SUN ⇒ 일
23:00〜 メン限配信 ⇒ 23:00~ 멤버십 한정 방송
illustration ⇒ illustration
ふろーる 様 @flowle_ow0 ⇒ 후로루 님 @flowle_ow0
Sorashina Sopia ⇒ Sorashina Sopia
宙科そぴあ ⇒ 소라시나 소피아
Sorashina Sopia ⇒ Sorashina Sopia`
	sophiaRender = `Weekly
Schedule
11/9 ~11/15
일정은 변경될
가능성이 있습니다
월
19:00~ 이과 여자 공감 콜라보
@하쿠이 코요리 선배 ch.
화
20:00~ 홀로도리 방송
수
20:00~ 콜라보 방송
목
22:00~ 술을 마시며 이야기 나누기
@주후테이 라덴 ch.
금
22:00~ 콜라보 방송
토
20:00~ 일치할 때까지 끝낼 수 없습니다!!
with 아소비★마와리타이! @아치치 메라 ch.
일
23:00~ 멤버십 한정 방송
illustration
후로루 님 @flowle_ow0
Sorashina Sopia
소라시나 소피아
Sorashina Sopia`
)

// 쿄코 자료의 원본 판독은 운영 로그에 남은 실제 판독(アソビ隊를 アンビ隊로 읽음, 포스터의 오타 sepnai 포함)에 장식 표시만 더한 것이다.
const (
	kyokoOriginal = `WEEKLY
SCHEDULE
장식: HYAKUTO
장식: KYOKO
장식: KEEP OUT
2026/11/09 - 11/15
*All times listed are in JST
9
MON
EN
12:00PM
PEAK
FULL SQUAD!!
JP
9:00PM
Overwatch
with つーちゃん
10
TUE
EN
12:00PM
Soul Chained
with Bijou-sepnai
Chihaya-senpai
11
WED
EN
11:00AM
Deadlock
with Gigi-senpai
JP
10:00PM
鬼武者
Way of the Sword
12
THU
EN
12:00AM
Cuphead
JP
10:00PM
デス桃太郎
イラスト: @ren_mon4 さん
13
FRI
EN
12:00PM
The Plant Shop
JP
10:00PM
Phasmophobia
with めらち
14
SAT
EN
12:00PM
FNAF 3
JP
20:00PM
終われまテン！
with アンビ隊
JP
10:00PM
ダークソウル
SL1ノーデス
15
SUN
EN
12:00PM
Collab
Raora-senpai
JP
10:00PM
地獄銭湯
雑談
歌枠
ゲーム
コラボ
その他
おやすみ
※ 配信時間や内容は諸事情で変更になる可能性があります。
장식: HYAKUTO KYOKO`
	kyokoTable = `WEEKLY SCHEDULE ⇒ 주간 스케줄
2026/11/09 - 11/15 ⇒ 2026/11/09 - 11/15
*All times listed are in JST ⇒ *모든 시간은 일본 시간(JST) 기준
9 ⇒ 9
MON ⇒ 월
EN ⇒ EN
12:00PM ⇒ 12:00PM
PEAK FULL SQUAD!! ⇒ PEAK 풀 스쿼드!!
JP ⇒ JP
9:00PM ⇒ 9:00PM
Overwatch with つーちゃん ⇒ Overwatch 츠-짱과 함께
10 ⇒ 10
TUE ⇒ 화
EN ⇒ EN
12:00PM ⇒ 12:00PM
Soul Chained with Bijou-sepnai Chihaya-senpai ⇒ Soul Chained 비쥬 선배, 치하야 선배와 함께
11 ⇒ 11
WED ⇒ 수
EN ⇒ EN
11:00AM ⇒ 11:00AM
Deadlock with Gigi-senpai ⇒ Deadlock 지지 선배와 함께
JP ⇒ JP
10:00PM ⇒ 10:00PM
鬼武者 Way of the Sword ⇒ 오니무샤 Way of the Sword
12 ⇒ 12
THU ⇒ 목
EN ⇒ EN
12:00AM ⇒ 12:00AM
Cuphead ⇒ Cuphead
JP ⇒ JP
10:00PM ⇒ 10:00PM
デス桃太郎 ⇒ 데스 모모타로
イラスト: @ren_mon4 さん ⇒ 일러스트: @ren_mon4 님
13 ⇒ 13
FRI ⇒ 금
EN ⇒ EN
12:00PM ⇒ 12:00PM
The Plant Shop ⇒ The Plant Shop
JP ⇒ JP
10:00PM ⇒ 10:00PM
Phasmophobia with めらち ⇒ Phasmophobia 메라치와 함께
14 ⇒ 14
SAT ⇒ 토
EN ⇒ EN
12:00PM ⇒ 12:00PM
FNAF 3 ⇒ FNAF 3
JP ⇒ JP
20:00PM ⇒ 20:00PM
終われまテン！ with アソビ隊 ⇒ 오와레마텐! 아소비타이와 함께
JP ⇒ JP
10:00PM ⇒ 10:00PM
ダークソウル SL1ノーデス ⇒ 다크 소울 SL1 노 데스
15 ⇒ 15
SUN ⇒ 일
EN ⇒ EN
12:00PM ⇒ 12:00PM
Collab Raora-senpai ⇒ 라오라 선배 콜라보
JP ⇒ JP
10:00PM ⇒ 10:00PM
地獄銭湯 ⇒ 지고쿠센토
雑談 ⇒ 잡담
歌枠 ⇒ 노래 방송
ゲーム ⇒ 게임
コラボ ⇒ 콜라보
その他 ⇒ 기타
おやすみ ⇒ 휴방
※ 配信時間や内容は諸事情で変更になる可能性があります。 ⇒ ※ 방송 시간이나 내용은 사정에 따라 변경될 수 있습니다.`
	kyokoRender = `주간
스케줄
장식: HYAKUTO
장식: KYOKO
2026/11/09 - 11/15
*모든 시간은 일본 시간(JST) 기준
9
월
EN
12:00PM
PEAK 풀 스쿼드!!
JP
9:00PM
Overwatch
츠-짱과 함께
10
화
EN
12:00PM
Soul Chained
비쥬 선배, 치하야 선배와 함께
11
수
EN
11:00AM
Deadlock
지지 선배와 함께
JP
10:00PM
오니무샤
Way of the Sword
12
목
EN
12:00AM
Cuphead
JP
10:00PM
데스 모모타로
일러스트: @ren_mon4 님
13
금
EN
12:00PM
The Plant Shop
JP
10:00PM
Phasmophobia
메라치와 함께
14
토
EN
12:00PM
FNAF 3
JP
20:00PM
오와레마텐!
아소비타이와 함께
JP
10:00PM
다크 소울
SL1 노 데스
15
일
EN
12:00PM
라오라 선배 콜라보
JP
10:00PM
지고쿠센토
잡담
노래 방송
게임
콜라보
기타
휴방
※ 방송 시간이나 내용은 사정에 따라 변경될 수 있습니다.
장식: HYAKUTO KYOKO`
)

func mustTable(t *testing.T, text string) translationTable {
	t.Helper()

	table, err := parseTranslationTable(text)
	require.NoError(t, err)

	return table
}

func readTranscript(t *testing.T, text string) transcript {
	t.Helper()

	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if !strings.HasPrefix(line, decorationPrefix) {
			lines[i] = contentPrefix + " " + line
		}
	}

	read, err := parseTranscript(strings.Join(lines, "\n"))
	require.NoError(t, err)

	return read
}

func TestCheckRenderCatchesDroppedTimesAndLeftoverOriginal(t *testing.T) {
	t.Parallel()

	problems := checkRender(mustTable(t, tsuzuriTable), readTranscript(t, tsuzuriOriginal), readTranscript(t, tsuzuriBrokenRender))

	for _, want := range []string{
		"‘21:00’가 3번 있어야 하는데 번역본에는 1번 있다",
		"‘18:00’가 2번 있어야 하는데 번역본에는 1번 있다",
		"‘20:00’가 1번 있어야 하는데 번역본에는 0번 있다",
		"‘19:00’가 1번 있어야 하는데 번역본에는 0번 있다",
		"‘21:30’가 1번 있어야 하는데 번역본에는 0번 있다",
		"‘VALORANT’가 3번 있어야 하는데 번역본에는 1번 있다",
		"번역표 줄 ‘21:00~ 잡담 or VALORANT’이 번역본에 다 그려지지 않았다",
		"번역표 줄 ‘목’이 번역본에 다 그려지지 않았다",
		"번역되지 않은 원문 글자 ‘ざつだん’가 번역본에 남았다",
		"번역되지 않은 원문 글자 ‘まわり隊’가 번역본에 남았다",
	} {
		require.Contains(t, problems, want)
	}
}

func TestCheckPassesFaithfulTranslations(t *testing.T) {
	t.Parallel()

	for name, fixture := range map[string]struct{ original, table, render string }{
		"tsuzuri": {tsuzuriOriginal, tsuzuriTable, tsuzuriGoodRender},
		"sophia":  {sophiaOriginal, sophiaTable, sophiaRender},
		"kyoko":   {kyokoOriginal, kyokoTable, kyokoRender},
	} {
		original := readTranscript(t, fixture.original)
		table := mustTable(t, fixture.table)

		require.Empty(t, checkTable(table, original), name)
		require.Empty(t, checkRender(table, original, readTranscript(t, fixture.render)), name)
	}

	require.Empty(t, checkRender(mustTable(t, sophiaTable), readTranscript(t, sophiaOriginal),
		readTranscript(t, strings.Replace(sophiaRender, "11/9 ~11/15", "11/9ー11/15", 1))), "판독이 줄표를 장음 부호로 읽어도 원문이 남은 것으로 보지 않는다")
}

func TestCheckRenderRejectsTextOutsideTheTable(t *testing.T) {
	t.Parallel()

	rendered := readTranscript(t, strings.Replace(sophiaRender, "Sorashina Sopia\n소라시나", "Sorashina Sopia 생일 축하\n소라시나", 1))

	require.Equal(t, []string{"번역표에 없는 한국어 글자 ‘생’, ‘일’, ‘축’, ‘하’가 번역본에 있다"},
		checkRender(mustTable(t, sophiaTable), readTranscript(t, sophiaOriginal), rendered))
}

func TestCheckRenderRejectsEnglishLeftUntranslatedOrProperNounsTranslated(t *testing.T) {
	t.Parallel()

	table := mustTable(t, kyokoTable)
	original := readTranscript(t, kyokoOriginal)

	untranslated := readTranscript(t, strings.Replace(kyokoRender, "Deadlock\n지지 선배와 함께", "Deadlock\nwith Gigi-senpai", 1))
	require.Contains(t, checkRender(table, original, untranslated), "번역표 줄 ‘Deadlock 지지 선배와 함께’이 번역본에 다 그려지지 않았다")

	transliterated := checkRender(table, original, readTranscript(t, strings.Replace(kyokoRender, "Cuphead", "컵헤드", 1)))
	require.Contains(t, transliterated, "‘CUPHEAD’가 1번 있어야 하는데 번역본에는 0번 있다")
	require.Contains(t, transliterated, "번역표에 없는 한국어 글자 ‘드’, ‘컵’, ‘헤’가 번역본에 있다")
}

func TestCheckTableRequiresEveryOriginalLine(t *testing.T) {
	t.Parallel()

	original := readTranscript(t, tsuzuriOriginal)

	dropped := mustTable(t, strings.Replace(tsuzuriTable, "21:00〜ざつだん or VALORANT ⇒ 21:00~ 잡담 or VALORANT\n", "", 1))
	require.Equal(t, []string{
		"원본에 3번 있는 ‘21:00’가 번역표의 한국어 줄에는 2번만 있다",
		"원본에 3번 있는 ‘VALORANT’가 번역표의 원문 줄에는 2번만 있다",
		"원본의 ‘ざつだん’ 부분이 번역표에 빠졌다",
	}, checkTable(dropped, original))

	repeated := mustTable(t, strings.Replace(tsuzuriTable, "21:30〜「時のオカリナ」 ⇒ 21:30~ 「시간의 오카리나」\n", "", 1))
	require.Equal(t, []string{
		"원본에 1번 있는 ‘21:30’가 번역표의 한국어 줄에는 0번만 있다",
		"원본의 ‘時のオカリナ’ 부분이 번역표에 빠졌다",
	}, checkTable(repeated, original), "같은 문구가 두 번 나오면 번역표에도 두 번 있어야 한다")

	require.Equal(t, []string{"원본의 ‘完売’ 부분이 번역표에 빠졌다"}, checkTable(mustTable(t, sophiaTable), readTranscript(t, sophiaOriginal+"\n完売")),
		"대신 읽은 글자 없이 빠진 원문은 두 글자라도 번역표에서 걸린다")

	broken := mustTable(t, "21:00〜ざつだん or VALORANT ⇒ 잡담 또는 발로란트\nSUN ⇒ 일\nTHU ⇒ THU")
	require.Equal(t, []string{"번역표 줄 ‘21:00〜ざつだん or VALORANT’의 한국어 줄에 원문의 ‘21:00’가 빠졌다"},
		checkTable(broken, transcript{content: []string{"SUN", "THU"}}), "영어는 옮길 수 있어도 숫자는 줄마다 그대로 있어야 한다")

	leftover := mustTable(t, "21:00〜ざつだん ⇒ 21:00~ ざつだん")
	require.Equal(t, []string{"번역표 줄 ‘21:00〜ざつだん’의 한국어 줄에 번역하지 않은 ‘ざつだん’가 남았다"},
		checkTable(leftover, transcript{content: []string{"21:00〜ざつだん"}}))
}

func TestCheckTableRequiresEnglishContentButNotDecorations(t *testing.T) {
	t.Parallel()

	original := readTranscript(t, kyokoOriginal)

	dropped := mustTable(t, strings.Replace(kyokoTable, "Deadlock with Gigi-senpai ⇒ Deadlock 지지 선배와 함께\n", "", 1))
	require.Equal(t, []string{
		"원본에 1번 있는 ‘DEADLOCK’가 번역표의 원문 줄에는 0번만 있다",
		"원본에 1번 있는 ‘GIGI’가 번역표의 원문 줄에는 0번만 있다",
		"원본에 3번 있는 ‘SENPAI’가 번역표의 원문 줄에는 2번만 있다",
		"원본에 5번 있는 ‘WITH’가 번역표의 원문 줄에는 4번만 있다",
	}, checkTable(dropped, original))

	corrected := mustTable(t, strings.Replace(kyokoTable, "Bijou-sepnai", "Bijou-senpai", 1))
	require.Empty(t, checkTable(corrected, original), "포스터의 오타를 번역표가 고쳐 읽어도 다르게 읽은 것으로 본다")

	logoAsContent := transcript{content: append(slices.Clone(original.content), "HYAKUTO KYOKO"), decoration: original.decoration}
	require.Equal(t, []string{
		"원본에 1번 있는 ‘HYAKUTO’가 번역표의 원문 줄에는 0번만 있다",
		"원본에 1번 있는 ‘KYOKO’가 번역표의 원문 줄에는 0번만 있다",
	}, checkTable(mustTable(t, kyokoTable), logoAsContent))
	require.Empty(t, checkTable(mustTable(t, kyokoTable+"\nHYAKUTO KYOKO ⇒ HYAKUTO KYOKO"), logoAsContent),
		"판독이 로고를 내용으로 읽어도 번역표가 그대로 적으면 통과한다")
}

func TestCheckTableToleratesSmallReadingDifferences(t *testing.T) {
	t.Parallel()

	table := mustTable(t, "休みになるかのうせい大 ⇒ 휴방이 될 가능성 큼\n一致するまで終われまテン ⇒ 일치할 때까지 끝낼 수 없텐")
	read := func(lines ...string) transcript { return transcript{content: lines} }

	require.Empty(t, checkTable(table, read("休みになる", "かのうせい大", "一致するまで終わればテン")),
		"줄 나눔이 다르거나 한 글자를 다르게 읽은 것은 같은 원문으로 본다")
	require.Empty(t, checkTable(mustTable(t, "休みになる ⇒ 휴방이 될\nかのうせい大 ⇒ 가능성 큼"), read("休みになるかのうせい大")),
		"판독이 한 줄로 읽은 것을 번역표가 두 줄로 나눠도 같은 원문으로 본다")
	require.Empty(t, checkTable(table, read("休みになるかのうせい大", "一致するまで終われテン")), "판독이 글자 하나를 빠뜨려도 같은 원문으로 본다")
	require.Empty(t, checkTable(mustTable(t, "@アソビ★まわり隊 めらch. ⇒ @아소비★마와리타이 메라 ch."), read("@アソビきまわり隊 めらch.")),
		"판독이 ★를 가나로 읽어도 번역표의 ★를 대신 읽은 글자로 본다")
	require.NotEmpty(t, checkTable(table, read("休みになる", "かのうせい大", "一致するまで終われまテン", "ざつだん")))
	require.Equal(t, []string{"원본의 ‘月’ 부분이 번역표에 빠졌다", "원본의 ‘火’ 부분이 번역표에 빠졌다", "원본의 ‘水’ 부분이 번역표에 빠졌다"},
		checkTable(table, read("月", "火", "水", "休みになるかのうせい大", "一致するまで終われまテン")), "한 글자 요일 한자도 번역표에 있어야 한다")
	require.Equal(t, []string{"원본의 ‘月’ 부분이 번역표에 빠졌다"},
		checkTable(table, transcript{content: read("休みになるかのうせい大", "一致するまで終われまテン").content, decoration: []string{"月"}}),
		"일본어는 장식으로 읽혀도 번역표에 있어야 한다")
}

func TestParseTranscriptKeepsOnlyMarkedLines(t *testing.T) {
	t.Parallel()

	read, err := parseTranscript("이미지의 글자입니다.\n읽음: 21:00~ 잡담\n- 장식: HYAKUTO KYOKO\n- 읽음: or\n읽음:\n참고: 끝")
	require.NoError(t, err)
	require.Equal(t, transcript{content: []string{"21:00~ 잡담", "or"}, decoration: []string{"HYAKUTO KYOKO"}}, read)

	_, err = parseTranscript("글자가 없습니다.")
	require.Error(t, err)
}
