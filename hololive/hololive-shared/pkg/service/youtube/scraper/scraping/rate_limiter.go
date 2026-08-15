package scraping

import (
	"net/url"
	"strings"
)

func distributedBucketFromURL(pageURL string) string {
	base := ytDefaults.DistributedRateLimit.BucketBase
	parsed, err := url.Parse(pageURL)
	if err != nil {
		return base + ":unknown"
	}
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		path = "root"
	}
	path = strings.ReplaceAll(path, "/", ":")
	return base + ":" + path
}
