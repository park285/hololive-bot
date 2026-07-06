package handlers

import "testing"

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
		{name: "unknown topic falls through to title", topic: "drawing", title: "【めっちゃカメレオン】参加型", want: BroadcastTypeGame},
		{name: "ambiguous observed topic falls through to title", topic: "morning", title: "【雑談】朝のんびり話す", want: BroadcastTypeTalk},
		{name: "ambiguous observed topic remains unknown without title evidence", topic: "Outfit_Reveal", title: "【緊急ゲリラ】ありがとう", want: BroadcastTypeUnknown},
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
		{name: "radio substring does not overmatch", title: "【Radioactive】science talk", want: BroadcastTypeUnknown},
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
		{name: "title source", topic: "drawing", title: "【雑談】", wantType: BroadcastTypeTalk, wantSource: "title"},
		{name: "membership title overrides game topic", topic: "minecraft", title: "【Members Only】 yuru camp △ s1 ep.7-12 ゆるキャン", wantType: BroadcastTypeMembership, wantSource: "title"},
		{name: "watchalong title overrides game topic", topic: "forza", title: "【同時視聴】映画をみんなで観よ", wantType: BroadcastTypeWatchalong, wantSource: "title"},
		{name: "horse racing title overrides game topic", topic: "minecraft", title: "【競馬/大阪杯】阪神 芝2000！！！今日こそ勝！！！！！！！！！", wantType: BroadcastTypeHorseRacing, wantSource: "title"},
		{name: "strong event title overrides game topic", topic: "pokemon", title: "【今夜19時】公認ポケモンチャンピオンズ大会!新たな歴史の一ページが生まれる…!?", wantType: BroadcastTypeEvent, wantSource: "title"},
		{name: "game topic keeps priority over talk title", topic: "minecraft", title: "【Minecraft】雑談しながら整地", wantType: BroadcastTypeGame, wantSource: "topic"},
		{name: "game topic keeps priority over generic event title", topic: "minecraft", title: "【Minecraft】ウォーデン100体もたおした!!大会もみた!次はおまえだ", wantType: BroadcastTypeGame, wantSource: "topic"},
		{name: "non-game topic keeps priority over game title", topic: "singing", title: "【Minecraft】歌いながら整地", wantType: BroadcastTypeSinging, wantSource: "topic"},
		{name: "unknown source", topic: "announce", title: "【緊急ゲリラ】", wantType: BroadcastTypeUnknown, wantSource: "unknown"},
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
