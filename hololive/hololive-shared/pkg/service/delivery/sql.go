package delivery

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed queries/*
var sqlAssets embed.FS

func mustSQL(name string) string {
	query, err := sqlAssets.ReadFile("queries/" + name)
	if err != nil {
		panic(err)
	}
	text := strings.TrimSpace(string(query))
	if text == "" {
		panic(fmt.Sprintf("empty SQL asset %s", name))
	}
	return text
}
