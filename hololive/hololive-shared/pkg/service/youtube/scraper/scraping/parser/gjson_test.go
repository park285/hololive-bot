package parser

import "github.com/tidwall/gjson"

func parseGJSONResultPtr(raw string) *gjson.Result {
	result := gjson.Parse(raw)
	return &result
}

func parseGJSONBytesResultPtr(raw []byte) *gjson.Result {
	result := gjson.ParseBytes(raw)
	return &result
}
