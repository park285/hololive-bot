package pollers

import "embed"

//go:embed queries/*
var sqlAssets embed.FS

func mustSQL(name string) string {
	query, err := sqlAssets.ReadFile("queries/" + name)
	if err != nil {
		panic(err)
	}
	return string(query)
}
