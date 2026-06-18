package scraping

import "github.com/tidwall/gjson"

func parseGJSONResultPtr(raw string) *gjson.Result {
	result := gjson.Parse(raw)
	return &result
}

func gjsonResultPtr(result *gjson.Result) *gjson.Result {
	return result
}
