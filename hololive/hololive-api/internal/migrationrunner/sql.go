package migrationrunner

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed queries/* patterns/*
var sqlAssets embed.FS

func mustSQL(name string) string {
	query, err := sqlAssets.ReadFile("queries/" + name)
	if err != nil {
		panic(err)
	}
	return nonEmptyAsset("SQL", name, string(query))
}

func mustPattern(name string) string {
	pattern, err := sqlAssets.ReadFile("patterns/" + name)
	if err != nil {
		panic(err)
	}
	return nonEmptyAsset("pattern", name, string(pattern))
}

func nonEmptyAsset(kind, name, value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		panic(fmt.Sprintf("empty %s asset %s", kind, name))
	}
	return text
}
