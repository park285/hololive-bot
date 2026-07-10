package handlers

import (
	"slices"
	"testing"
)

func TestBroadcastRuleOrderPinned(t *testing.T) {
	t.Parallel()

	wantStrong := []BroadcastType{
		BroadcastTypeMembership,
		BroadcastTypeWatchalong,
		BroadcastTypeSinging,
		BroadcastTypeNews,
		BroadcastTypeASMR,
		BroadcastTypeHorseRacing,
		BroadcastTypeEvent,
		BroadcastTypeEvent,
		BroadcastTypeNews,
	}
	gotStrong := make([]BroadcastType, 0, len(broadcastRules.TitleRules))
	for _, rule := range broadcastRules.TitleRules {
		gotStrong = append(gotStrong, rule.Type)
	}
	if !slices.Equal(gotStrong, wantStrong) {
		t.Fatalf("title_rules order = %v, want %v", gotStrong, wantStrong)
	}

	wantGeneric := []BroadcastType{
		BroadcastTypeGame,
		BroadcastTypeEvent,
		BroadcastTypeSinging,
		BroadcastTypeTalk,
		BroadcastTypeOther,
		BroadcastTypeNews,
	}
	gotGeneric := make([]BroadcastType, 0, len(broadcastRules.Generic))
	for _, rule := range broadcastRules.Generic {
		gotGeneric = append(gotGeneric, rule.Type)
	}
	if !slices.Equal(gotGeneric, wantGeneric) {
		t.Fatalf("generic_title_rules order = %v, want %v", gotGeneric, wantGeneric)
	}
}

func TestClassifyBroadcastObservedTopics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		topic string
		title string
		want  BroadcastType
	}{
		{name: "observed game topic", topic: "Forza", want: BroadcastTypeGame},
		{name: "observed news topic", topic: "News_Show", want: BroadcastTypeNews},
		{name: "observed membership topic", topic: "membersonly", want: BroadcastTypeMembership},
		{name: "observed other topic", topic: "Vlog", want: BroadcastTypeOther},
		{name: "observed outfit reveal topic", topic: "Outfit_Reveal", want: BroadcastTypeEvent},
		{name: "observed instrument topic", topic: "Musical_Instrument", want: BroadcastTypeSinging},
		{name: "ambiguous announce topic falls through to title", topic: "announce", title: "【緊急ゲリラ】ガチャガチャ屋さんの店長になりました【Gacha Capsule Shop Simulator - Akihabara】", want: BroadcastTypeGame},
		{name: "ambiguous drawing topic falls through to title", topic: "drawing", title: "【めっちゃカメレオン】自分を塗って景色に溶け込むお絵描きかくれんぼゲーム!", want: BroadcastTypeGame},
		{name: "unknown topic falls through to title", topic: "endurance", title: "【めっちゃカメレオン】参加型", want: BroadcastTypeGame},
		{name: "ambiguous observed topic falls through to title", topic: "morning", title: "【雑談】朝のんびり話す", want: BroadcastTypeTalk},
		{name: "ambiguous observed topic remains unknown without title evidence", topic: "morning", title: "【緊急ゲリラ】ありがとう", want: BroadcastTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyBroadcast(tt.topic, tt.title); got != tt.want {
				t.Fatalf("ClassifyBroadcast(%q, %q) = %q, want %q", tt.topic, tt.title, got, tt.want)
			}
		})
	}
}

func TestClassifyBroadcastTitleFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  BroadcastType
	}{
		{name: "membership has access priority", title: "【Members Only】 yuru camp △ s1 ep.7-12 ゆるキャン", want: BroadcastTypeMembership},
		{name: "watchalong beats asmr", title: "【同時視聴】脳がとろける♡「ゼットンの甘々ASMR」みんなで観よ♩", want: BroadcastTypeWatchalong},
		{name: "3d karaoke is singing", title: "【 3Dカラオケ】お子様バッテリー1周年記念にカラオケき~たよ", want: BroadcastTypeSinging},
		{name: "horse racing race name", title: "【 大阪杯 】強豪揃いの大阪杯…‼的中したいぜ！！！！！！！【鷹嶺ルイ/ホロライブ】", want: BroadcastTypeHorseRacing},
		{name: "horse racing challenge event", title: "【ホロライブ 的中チャレンジバトル】DAY1チームトップバッター行きます‼ #ホロ的中バトル", want: BroadcastTypeHorseRacing},
		{name: "jra g1 full race name", title: "【有馬記念】今年最後のG1をみんなで予想する", want: BroadcastTypeHorseRacing},
		{name: "jra g1 abbreviation", title: "【NHKマイルC】本命を決めるぞ", want: BroadcastTypeHorseRacing},
		{name: "overseas observed race name", title: "【サウジカップ】フォーエバーヤング2連覇なるか⁉", want: BroadcastTypeHorseRacing},
		{name: "nar dirt g1 is horse racing", title: "【帝王賞】大井の帝王決定戦を予想する", want: BroadcastTypeHorseRacing},
		{name: "year end dirt g1 is horse racing", title: "【東京大賞典】今年のダート王を決めよう", want: BroadcastTypeHorseRacing},
		{name: "arc de triomphe is horse racing", title: "【凱旋門賞】日本馬の悲願なるか", want: BroadcastTypeHorseRacing},
		{name: "stakes long form race name", title: "【根岸ステークス】ダート短距離戦線開幕", want: BroadcastTypeHorseRacing},
		{name: "full width race name normalizes", title: "【天皇賞（秋）】天覧競走を大予想", want: BroadcastTypeHorseRacing},
		{name: "uma musume race title is game", title: "【ウマ娘】有馬記念を勝ちたい！！", want: BroadcastTypeGame},
		{name: "uma musume anniversary title is game", title: "【ウマ娘】3周年記念キャンペーンを走る", want: BroadcastTypeGame},
		{name: "bare uma musume race title is game", title: "ウマ娘 有馬記念を完全攻略していく", want: BroadcastTypeGame},
		{name: "winning post race title is game", title: "【ウイニングポスト10】凱旋門賞制覇への道", want: BroadcastTypeGame},
		{name: "winning post compact tag is game", title: "【Winning Post10】凱旋門賞への道", want: BroadcastTypeGame},
		{name: "birthday event with uma musume stays event", title: "【生誕祭】ウマ娘やる！", want: BroadcastTypeEvent},
		{name: "graduation with uma musume stays event", title: "【卒業配信】ウマ娘ありがとう", want: BroadcastTypeEvent},
		{name: "race lead tag with uma musume mention stays horse racing", title: "【有馬記念】ウマ娘声優さんと予想する", want: BroadcastTypeHorseRacing},
		{name: "full width street fighter tag is game", title: "【スト６】ランクマ潜る", want: BroadcastTypeGame},
		{name: "bare prediction wording is not horse racing", title: "【クイズ】全問的中させたい", want: BroadcastTypeUnknown},
		{name: "bare g1 wording is not horse racing", title: "【G1】ランク到達するまで終われない", want: BroadcastTypeUnknown},
		{name: "nte substring in content does not overmatch", title: "NEXT CONTENT PLANNING", want: BroadcastTypeUnknown},
		{name: "nte substring in interviewed does not overmatch", title: "We interviewed the director of [Project Hail Mary]! Includes a discussion with Marin", want: BroadcastTypeUnknown},
		{name: "nte exact title tag is game", title: "【NTE】Neverness to Evernessを遊ぶ", want: BroadcastTypeGame},
		{name: "neverness full title is game", title: "【NTE: Neverness to Everness】新作ゲームプレイさせていただく", want: BroadcastTypeGame},
		{name: "talk keyword", title: "【雑談】みんなにゲーム教えてもらう会", want: BroadcastTypeTalk},
		{name: "game title marker", title: "【リズム天国ミラクルスターズ】新作きちゃ!リズム天国ミラクルスターズ!", want: BroadcastTypeGame},
		{name: "slash in game title marker", title: "【バイオハザードRE4/Resident Evil】初見プレイ", want: BroadcastTypeGame},
		{name: "game marker outside first title tag", title: "【#VSPOEN】Faceit | Counter-Strike 2", want: BroadcastTypeGame},
		{name: "game marker inside hashtag title tag", title: "【#AdVSJus GEOGUESSER】no guesses needed to know justice loses", want: BroadcastTypeGame},
		{name: "delta force title marker", title: "【Delta Force】本番！！！がっぽり稼ぐぞ！！！！", want: BroadcastTypeGame},
		{name: "little nightmares full width digit", title: "【 リトルナイトメア２】ネタバレ有‼️キーマウの力を見せつけるその2", want: BroadcastTypeGame},
		{name: "hades roman numeral title marker", title: "#6【Hades II】Short Stream Because I Overslept", want: BroadcastTypeGame},
		{name: "plate up punctuation title marker", title: "【PLATE UP!】祭の練習じゃ～～～～", want: BroadcastTypeGame},
		{name: "super smash title marker", title: "【Super Smash Bros. Ultimate】HELP", want: BroadcastTypeGame},
		{name: "minecraft tournament mention is game", title: "【Minecraft】ウォーデン100体もたおした!!大会もみた!次はおまえだ", want: BroadcastTypeGame},
		{name: "valorant tournament prep is game", title: "【VALORANT】二日後大会の人のソロコンペがこちら", want: BroadcastTypeGame},
		{name: "resident evil endurance is game", title: "【バイオハザード HDリマスター】クリア耐久!完全初見!初代バイオいくぞ", want: BroadcastTypeGame},
		{name: "super mario endurance is game", title: "【スーパーマリオギャラクシー2】完全初見！クリア耐久!?へたっぴマリギャラ2！", want: BroadcastTypeGame},
		{name: "official pokemon tournament is event", title: "【今夜19時】公認ポケモンチャンピオンズ大会!新たな歴史の一ページが生まれる…!?", want: BroadcastTypeEvent},
		{name: "holomario tournament is event", title: "【#ホロマリオテニス大会】本番！！！全力で勝つぺこ！", want: BroadcastTypeEvent},
		{name: "exact lol tag is game", title: "【LOL】フルパでランク", want: BroadcastTypeGame},
		{name: "league of legends tag is game", title: "【League of Legends】今日もランク", want: BroadcastTypeGame},
		{name: "ff substring does not overmatch", title: "【OFF COLLAB】近況報告", want: BroadcastTypeUnknown},
		{name: "cooking title marker is other", title: "【OFF COLLAB】料理する", want: BroadcastTypeOther},
		{name: "member tag is not game", title: "新しいマイクに変えた(テスト配信)【ぶいすぽ / 猫汰つな】", want: BroadcastTypeUnknown},
		{name: "generic emergency tag is not game", title: "【緊急ゲリラ】ガチャガチャ屋さんの店長になりました", want: BroadcastTypeUnknown},
		{name: "chat substring does not overmatch", title: "【Chatterbox】new mic test", want: BroadcastTypeUnknown},
		{name: "radio substring does not overmatch", title: "【Radioactive】science stuff", want: BroadcastTypeUnknown},
		{name: "sequel digit keeps game keyword", title: "【Portal2】協力するぞ！ #ポルーナ のポータル2！", want: BroadcastTypeGame},
		{name: "gta with digit keeps game keyword", title: "【GTA5│NEW TOWN】Day2 街ブラ散歩", want: BroadcastTypeGame},
		{name: "ascii keyword adjacent to kana matches", title: "【PUBGモバイル】PUBGモバイルに余が参戦・・・！？", want: BroadcastTypeGame},
		{name: "ascii keyword after kana matches", title: "おひさしR.E.P.O", want: BroadcastTypeGame},
		{name: "zatsudan suffix matches talk", title: "【zatsudan】good morning, おはよ", want: BroadcastTypeTalk},
		{name: "hollow bracket lead tag matches exact game tag", title: "〖 OW 〗低気圧なのでチル。の巻", want: BroadcastTypeGame},
		{name: "corner bracket watch party is watchalong", title: "◤ #VSPO_SHOWDOWN　ウォチパ ◢　Day1 LOL 先輩たちと見ます！", want: BroadcastTypeWatchalong},
		{name: "double angle bracket game marker", title: "≪Devil May Cry 3≫ I'm ready.... First Playthrough! #1", want: BroadcastTypeGame},
		{name: "watch party compound tag is watchalong", title: "【#WatchPartyLCP】DCG vs GZ | LCP 2026 Split 1 Knockout Stage Day 4", want: BroadcastTypeWatchalong},
		{name: "fes support room is watchalong", title: "【応援枠】hololive SUPER EXPO 2026＆hololive 7th fes.お疲れ様でした！", want: BroadcastTypeWatchalong},
		{name: "membership katakana long form", title: "【メンバーシップ限定】ASMRささやき✨️最初だけ全体公開🎧️", want: BroadcastTypeMembership},
		{name: "birthday countdown is event", title: "【 #桃鈴ねね誕生日ライブ2026 】今年もみんなと迎えたい！お誕生日！！！", want: BroadcastTypeEvent},
		{name: "uma musume birthday title stays game", title: "【ウマ娘】誕生日ミッションを走る", want: BroadcastTypeGame},
		{name: "major report is news", title: "【重大報告】一日小豆警察署長 に就任しました！", want: BroadcastTypeNews},
		{name: "generic horror game evidence", title: "【悪意】誰かに見られてる?間違いなく今年一怖いと噂のホラーゲームらしい――。", want: BroadcastTypeGame},
		{name: "generic game word last resort", title: "超話題ゲーム『ゼットンの1兆度ホームラン競争』、遊ぶぞ！", want: BroadcastTypeGame},
		{name: "fes aftertalk is talk", title: "7th fesお疲れ様でした!! アフタートーク🎤✨", want: BroadcastTypeTalk},
		{name: "instrument performance is singing", title: "ウクレレを弾くだけの配信", want: BroadcastTypeSinging},
		{name: "sponsored tag last resort is other", title: "【DISM】肌のキャラ対してる！？メディスキンケア！ #ad", want: BroadcastTypeOther},
		{name: "announcement last resort is news", title: "【告知】あのグッズ、復刻します！！！", want: BroadcastTypeNews},
		{name: "appended notice keeps event classification", title: "【告知あり】ドキドキ凸待ちしてみる…！", want: BroadcastTypeEvent},
		{name: "appended notice in body is not news", title: "【ぐだぐだ】今後について、告知ありです", want: BroadcastTypeUnknown},
		{name: "big announcement aside stays talk", title: "【雑談】今後の活動について、重大発表あり！", want: BroadcastTypeTalk},
		{name: "participation zatsudan stays talk", title: "【参加型雑談】みんなでお話", want: BroadcastTypeTalk},
		{name: "birthday radio episode stays talk", title: "『 #誕生日にもらってスゴかったもの  💕』 アキちょこナイトパレット第26回 ～ホロライブ深夜ラジオ～", want: BroadcastTypeTalk},
		{name: "news show about asmr unlock stays news", title: "【昇天】ダニィ！？ヴィヴィさんがASMR解禁だと！？【昼ホロ/井月みちる】", want: BroadcastTypeNews},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyBroadcast("", tt.title); got != tt.want {
				t.Fatalf("ClassifyBroadcast(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestClassifyBroadcastSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		topic      string
		title      string
		wantType   BroadcastType
		wantSource string
	}{
		{name: "topic source", topic: "singing", title: "【雑談】", wantType: BroadcastTypeSinging, wantSource: "topic"},
		{name: "title source", topic: "endurance", title: "【雑談】", wantType: BroadcastTypeTalk, wantSource: "title"},
		{name: "membership title overrides game topic", topic: "minecraft", title: "【Members Only】 yuru camp △ s1 ep.7-12 ゆるキャン", wantType: BroadcastTypeMembership, wantSource: "title"},
		{name: "watchalong title overrides game topic", topic: "forza", title: "【同時視聴】映画をみんなで観よ", wantType: BroadcastTypeWatchalong, wantSource: "title"},
		{name: "horse racing title overrides game topic", topic: "minecraft", title: "【競馬/大阪杯】阪神 芝2000！！！今日こそ勝！！！！！！！！！", wantType: BroadcastTypeHorseRacing, wantSource: "title"},
		{name: "strong event title overrides game topic", topic: "pokemon", title: "【今夜19時】公認ポケモンチャンピオンズ大会!新たな歴史の一ページが生まれる…!?", wantType: BroadcastTypeEvent, wantSource: "title"},
		{name: "personal event title overrides game topic despite uma musume", topic: "minecraft", title: "【生誕祭】ウマ娘やる！", wantType: BroadcastTypeEvent, wantSource: "title"},
		{name: "game topic keeps priority over talk title", topic: "minecraft", title: "【Minecraft】雑談しながら整地", wantType: BroadcastTypeGame, wantSource: "topic"},
		{name: "game topic keeps priority over generic event title", topic: "minecraft", title: "【Minecraft】ウォーデン100体もたおした!!大会もみた!次はおまえだ", wantType: BroadcastTypeGame, wantSource: "topic"},
		{name: "non-game topic keeps priority over game title", topic: "singing", title: "【Minecraft】歌いながら整地", wantType: BroadcastTypeSinging, wantSource: "topic"},
		{name: "unknown source", topic: "endurance", title: "【緊急ゲリラ】", wantType: BroadcastTypeUnknown, wantSource: "unknown"},
		{name: "instrument title does not override game topic", topic: "minecraft", title: "【マイクラ】ピアノ作ってみた！", wantType: BroadcastTypeGame, wantSource: "topic"},
		{name: "generic news does not override game topic", topic: "valorant", title: "大事な告知があります！ランク行く", wantType: BroadcastTypeGame, wantSource: "topic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyBroadcastWithSource(tt.topic, tt.title)
			if got.Type != tt.wantType || got.Source != tt.wantSource {
				t.Fatalf("ClassifyBroadcastWithSource(%q, %q) = {%q %q}, want {%q %q}", tt.topic, tt.title, got.Type, got.Source, tt.wantType, tt.wantSource)
			}
		})
	}
}
