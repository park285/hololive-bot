package providerhttp

import "regexp"

var urlQueryPattern = regexp.MustCompile(`\?[^ \t"]*`)

func stripURLQuery(text string) string {
	return urlQueryPattern.ReplaceAllString(text, "")
}
