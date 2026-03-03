package command

const defaultMemberDirectoryGroup = "기타"

var memberDirectoryPreferredOrder = []string{
	"Advent",
	"FLOW GLOW",
	"Justice",
	"Myth",
	"Promise",
	"ReGLOSS",
	"비밀결사 holoX",
	"홀로라이브 0기생",
	"홀로라이브 1기생",
	"홀로라이브 2기생",
	"홀로라이브 3기생",
	"홀로라이브 4기생",
	"홀로라이브 5기생",
	"홀로라이브 게이머즈",
	"홀로라이브 인도네시아",
}

var memberDirectoryGroupAliases = map[string]string{
	"秘密結社holoX":                       "비밀결사 holoX",
	"ホロライブ0期生":                        "홀로라이브 0기생",
	"ホロライブ1期生":                        "홀로라이브 1기생",
	"ホロライブ2期生":                        "홀로라이브 2기생",
	"ホロライブ3期生":                        "홀로라이브 3기생",
	"ホロライブ4期生":                        "홀로라이브 4기생",
	"ホロライブ5期生":                        "홀로라이브 5기생",
	"ホロライブゲーマーズ":                      "홀로라이브 게이머즈",
	"ホロライブインドネシア":                     "홀로라이브 인도네시아",
	"ホロライブインドネシア（hololive Indonesia）": "홀로라이브 인도네시아",
	"Myth（神話）":                        "Myth",
	"Promise（約束）":                     "Promise",
	"ホロライブEnglish -Myth-":             "Myth",
	"ホロライブEnglish -Promise-":          "Promise",
	"hololive English Myth":           "Myth",
	"hololive English Promise":        "Promise",
}
