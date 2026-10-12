package celebration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBirthdayStreamTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  bool
	}{
		{title: "【BIRTHDAY COUNTDOWN】 6 YEARS OF BIRTHDAYS WITH YOU!! 【Kureiji Ollie】", want: true},
		{title: "【#白上フブキ生誕祭2026】みんなありがとう！", want: true},
		{title: "【お誕生日】今年もお祝いしてくれる？", want: true},
		{title: "【ﾊﾞｰｽﾃﾞｰﾗｲﾌﾞ】", want: true},
		{title: "【バースディライブ】", want: true},
		{title: "【#おたんじょうび】", want: true},
		{title: "お誕生会するよ", want: true},
		{title: "【ＢＩＲＴＨＤＡＹ ＬＩＶＥ】", want: true},
		{title: "Ollie's BDAY stream", want: true},
		{title: "【ULANG  TAHUN】 rayakan bersama!", want: true},
		{title: "생일 방송", want: true},
		{title: "【SARUSHIMA】 Jalan-jalan di pulau Sarushima?! 【VLOG】", want: false},
		{title: "【hololive Dreams】 DON'T FORGET TO LOGIN TODAY!! #holodori #ホロドリ", want: false},
		{title: "【歌枠】誕生石の話をしながら歌う", want: false},
		{title: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isBirthdayStreamTitle(tt.title))
		})
	}
}
