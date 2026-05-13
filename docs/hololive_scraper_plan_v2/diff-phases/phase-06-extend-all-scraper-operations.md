# Phase 06. 모든 scraper operation에 guard/snapshot 패턴 확장

## 목표

Phase 02~05에서는 upcoming 중심으로 시작했습니다. 이 phase에서는 같은 패턴을 다음 operation으로 확장합니다.

- recent videos
- shorts
- community posts
- channel stats
- channel snippet
- popular videos

## 코드 레벨 의사결정

1. operation마다 source를 명확히 둡니다.
   - HTML page: `FailureSourceHTML`
   - RSS feed: `FailureSourceRSS`

2. parser drift stage를 세분화합니다.
   - `extract_yt_initial_data`
   - `parse_initial_data`
   - `parse_rss_feed`
   - `check_alerts`

3. success 기록은 parse 성공 후에만 합니다.
   - fetch 성공했지만 parse 실패한 경우 성공으로 기록하면 안 됩니다.

4. 정상적인 “탭 없음/기능 미지원”은 parser drift가 아닙니다.
   - community tab missing은 기존 `communityMissing` state를 유지합니다.

## 변경 대상

- `scraper/videos.go`
- `scraper/shorts.go`
- `scraper/community.go`
- `scraper/channel.go`

## Diff: videos.go

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go b/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
index eeeeeee..1116666 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/videos.go
@@
 func (c *Client) getRecentVideosFromPage(ctx context.Context, pageURL, channelID string, maxResults int) ([]*Video, error) {
-	html, err := c.fetchPage(ctx, pageURL)
+	html, err := c.fetchChannelSourcePage(ctx, "recent_videos", channelID, pageURL, FailureSourceHTML)
 	if err != nil {
 		return nil, err
 	}

 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
 		logStructureWarning("recent_videos", channelID, "ytInitialData extraction failed", "error", err)
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		return nil, c.recordParserDrift(ctx, "recent_videos", "extract_yt_initial_data", channelID, pageURL, FailureSourceHTML, html, err)
 	}

 	data := gjson.Parse(jsonStr)
 	videos, err := parseVideosFromInitialData(data, channelID, maxResults, c.parseVideoRenderer)
 	if err != nil {
 		logStructureWarning("recent_videos", channelID, "failed to parse initial data", "error", err)
-		return nil, err
+		return nil, c.recordParserDrift(ctx, "recent_videos", "parse_initial_data", channelID, pageURL, FailureSourceHTML, html, err)
 	}
+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
 	return videos, nil
 }
@@
 func (c *Client) getRecentVideosFromRSS(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
@@
 	rssURL := fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?channel_id=%s", channelID)
-	html, err := c.fetchPage(ctx, rssURL)
+	html, err := c.fetchChannelSourcePage(ctx, "recent_videos_rss", channelID, rssURL, FailureSourceRSS)
 	if err != nil {
 		return nil, err
 	}
 	videos, err := parseVideosFromRSSFeed(html, channelID, maxResults)
 	if err != nil {
-		return nil, err
+		return nil, c.recordParserDrift(ctx, "recent_videos_rss", "parse_rss_feed", channelID, rssURL, FailureSourceRSS, html, err)
 	}
+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceRSS)
 	return videos, nil
 }
@@
 func (c *Client) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*Video, error) {
 	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)
-	html, err := c.fetchPage(ctx, url)
+	html, err := c.fetchChannelSourcePage(ctx, "popular_videos", channelID, url, FailureSourceHTML)
 	if err != nil {
 		return nil, err
 	}

 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		return nil, c.recordParserDrift(ctx, "popular_videos", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
 	}

 	data := gjson.Parse(jsonStr)
 	if err := checkAlerts(data); err != nil {
 		return nil, err
@@
 	}

+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
 	return videos, nil
 }
```

## Diff: shorts.go

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/shorts.go b/hololive/hololive-shared/pkg/service/youtube/scraper/shorts.go
index 7e2cbb2..2226666 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/shorts.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/shorts.go
@@
 func (c *Client) GetShorts(ctx context.Context, channelID string, maxResults int) ([]*Short, error) {
 	url := fmt.Sprintf("https://www.youtube.com/channel/%s/shorts", channelID)
-	html, err := c.fetchPage(ctx, url, HighFrequencyChannelFetchPolicy)
+	html, err := c.fetchChannelSourcePage(ctx, "shorts", channelID, url, FailureSourceHTML, HighFrequencyChannelFetchPolicy)
 	if err != nil {
 		return nil, err
 	}

 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		return nil, c.recordParserDrift(ctx, "shorts", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
 	}

 	data := gjson.Parse(jsonStr)
 	if err := checkAlerts(data); err != nil {
 		return nil, err
 	}

 	shortItems := extractShortsLockupViewModels(data)
-	return c.parseShortsLockupViewModels(shortItems, maxResults), nil
+	shorts := c.parseShortsLockupViewModels(shortItems, maxResults)
+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
+	return shorts, nil
 }
```

## Diff: community.go

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/community.go b/hololive/hololive-shared/pkg/service/youtube/scraper/community.go
index 3e18c0e..3336666 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/community.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/community.go
@@
 func (c *Client) GetCommunityPosts(ctx context.Context, channelID string, maxResults int) ([]*CommunityPost, error) {
+	if err := c.ensureChannelSourceAllowed(ctx, channelID, FailureSourceHTML); err != nil {
+		return nil, err
+	}
 	if c.isCommunityMissing(ctx, channelID) {
 		return []*CommunityPost{}, nil
 	}
@@
 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
 		logStructureWarning("community_posts", channelID, "ytInitialData extraction failed", "error", err)
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		url := fmt.Sprintf("https://www.youtube.com/channel/%s/posts", channelID)
+		return nil, c.recordParserDrift(ctx, "community_posts", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
 	}
@@
 	if !postsContent.Exists() {
 		c.markCommunityMissing(ctx, channelID)
 		return nil, nil
 	}

-	return c.parseCommunityPosts(postsContent, maxResults), nil
+	posts := c.parseCommunityPosts(postsContent, maxResults)
+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
+	return posts, nil
 }
@@
 func (c *Client) fetchCommunityPostsPage(ctx context.Context, channelID string) (string, bool, error) {
 	url := fmt.Sprintf("https://www.youtube.com/channel/%s/posts", channelID)
-	html, err := c.fetchPage(ctx, url, HighFrequencyChannelFetchPolicy)
+	html, err := c.fetchChannelSourcePage(ctx, "community_posts", channelID, url, FailureSourceHTML, HighFrequencyChannelFetchPolicy)
 	if err == nil {
 		return html, false, nil
 	}
```

주의: `GetCommunityPosts`에서 `ensureChannelSourceAllowed`를 추가하고, `fetchCommunityPostsPage`에서도 `fetchChannelSourcePage`가 다시 호출합니다. 중복 guard가 싫다면 `GetCommunityPosts`의 첫 guard는 제거해도 됩니다. 추천은 `fetchCommunityPostsPage`에만 두는 것입니다.

## Diff: channel.go

```diff
diff --git a/hololive/hololive-shared/pkg/service/youtube/scraper/channel.go b/hololive/hololive-shared/pkg/service/youtube/scraper/channel.go
index 1c0de59..4446666 100644
--- a/hololive/hololive-shared/pkg/service/youtube/scraper/channel.go
+++ b/hololive/hololive-shared/pkg/service/youtube/scraper/channel.go
@@
 func (c *Client) GetChannelStats(ctx context.Context, channelID string) (*ChannelStats, error) {
 	url := fmt.Sprintf("https://www.youtube.com/channel/%s/about", channelID)

-	html, err := c.fetchPage(ctx, url)
+	html, err := c.fetchChannelSourcePage(ctx, "channel_stats", channelID, url, FailureSourceHTML)
 	if err != nil {
 		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
 	}

 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
 		logStructureWarning("channel_stats", channelID, "ytInitialData extraction failed", "error", err)
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		return nil, c.recordParserDrift(ctx, "channel_stats", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
 	}
@@
 	if looksEmptyChannelStats(stats) {
 		logStructureWarning("channel_stats", channelID, "parsed stats are empty")
 	}

+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
 	return stats, nil
 }
@@
 func (c *Client) GetChannelSnippet(ctx context.Context, channelID string) (*ChannelSnippet, error) {
 	url := fmt.Sprintf("https://www.youtube.com/channel/%s", channelID)

-	html, err := c.fetchPage(ctx, url)
+	html, err := c.fetchChannelSourcePage(ctx, "channel_snippet", channelID, url, FailureSourceHTML)
 	if err != nil {
 		return nil, fmt.Errorf("failed to fetch channel page: %w", err)
 	}

 	jsonStr, err := extractYtInitialData(html)
 	if err != nil {
 		logStructureWarning("channel_snippet", channelID, "ytInitialData extraction failed", "error", err)
-		return nil, fmt.Errorf("failed to extract ytInitialData: %w", err)
+		return nil, c.recordParserDrift(ctx, "channel_snippet", "extract_yt_initial_data", channelID, url, FailureSourceHTML, html, err)
 	}
@@
 	if len(snippet.Avatar) == 0 || len(snippet.Banner) == 0 {
@@
 	}

+	c.recordChannelSourceSuccess(ctx, channelID, FailureSourceHTML)
 	return snippet, nil
 }
```

## 실행

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/scraper
go test ./hololive/hololive-shared/pkg/service/youtube/poller
```

## 완료 기준

- 모든 HTML operation이 `fetchChannelSourcePage`를 통해 실패를 health에 기록합니다.
- 모든 parser drift가 `recordParserDrift`를 통해 snapshot 후보가 됩니다.
- RSS 실패는 HTML failure와 분리됩니다.
- community missing은 parser drift로 오염되지 않습니다.
