package migrationrunner

import "embed"

//go:embed queries/* patterns/*
var sqlAssets embed.FS

func mustSQL(name string) string {
	query, err := sqlAssets.ReadFile("queries/" + name)
	if err != nil {
		panic(err)
	}
	return string(query)
}

func mustPattern(name string) string {
	pattern, err := sqlAssets.ReadFile("patterns/" + name)
	if err != nil {
		panic(err)
	}
	return string(pattern)
}
