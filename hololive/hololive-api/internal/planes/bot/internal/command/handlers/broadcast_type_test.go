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
		{name: "talk keyword", title: "【雑談】みんなにゲーム教えてもらう会", want: BroadcastTypeTalk},
		{name: "game title marker", title: "【リズム天国ミラクルスターズ】新作きちゃ!リズム天国ミラクルスターズ!", want: BroadcastTypeGame},
		{name: "slash in game title marker", title: "【バイオハザードRE4/Resident Evil】初見プレイ", want: BroadcastTypeGame},
		{name: "game marker outside first title tag", title: "【#VSPOEN】Faceit | Counter-Strike 2", want: BroadcastTypeGame},
		{name: "exact lol tag is game", title: "【LOL】フルパでランク", want: BroadcastTypeGame},
		{name: "league of legends tag is game", title: "【League of Legends】今日もランク", want: BroadcastTypeGame},
		{name: "ff substring does not overmatch", title: "【OFF COLLAB】近況報告", want: BroadcastTypeUnknown},
		{name: "cooking title marker is other", title: "【OFF COLLAB】料理する", want: BroadcastTypeOther},
		{name: "member tag is not game", title: "新しいマイクに変えた(テスト配信)【ぶいすぽ / 猫汰つな】", want: BroadcastTypeUnknown},
		{name: "generic emergency tag is not game", title: "【緊急ゲリラ】ガチャガチャ屋さんの店長になりました", want: BroadcastTypeUnknown},
		{name: "chat substring does not overmatch", title: "【Chatterbox】new mic test", want: BroadcastTypeUnknown},
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
		{name: "game topic keeps priority over talk title", topic: "minecraft", title: "【Minecraft】雑談しながら整地", wantType: BroadcastTypeGame, wantSource: "topic"},
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
