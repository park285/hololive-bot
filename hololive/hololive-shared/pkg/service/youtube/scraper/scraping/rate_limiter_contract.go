package scraping

// RateLimiterConfigured reports whether every request issued by this client is
// admitted through a shared YouTube request limiter.
func (c *Client) RateLimiterConfigured() bool {
	return c != nil && c.rateLimiter != nil
}
