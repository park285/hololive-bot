package chzzk

type OpenAPIResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Content T      `json:"content,omitempty"`
}

type LivesResponse struct {
	Data []LiveData `json:"data"`
	Page PageInfo   `json:"page"`
}

type LiveData struct {
	LiveID                int      `json:"liveId"`
	LiveTitle             string   `json:"liveTitle"`
	LiveThumbnailImageURL string   `json:"liveThumbnailImageUrl"`
	ConcurrentUserCount   int      `json:"concurrentUserCount"`
	OpenDate              string   `json:"openDate"`
	Adult                 bool     `json:"adult"`
	Tags                  []string `json:"tags"`
	CategoryType          string   `json:"categoryType"`
	LiveCategory          string   `json:"liveCategory"`
	LiveCategoryValue     string   `json:"liveCategoryValue"`
	ChannelID             string   `json:"channelId"`
	ChannelName           string   `json:"channelName"`
	ChannelImageURL       string   `json:"channelImageUrl"`
}

type PageInfo struct {
	Next string `json:"next,omitempty"`
}

type ChannelsResponse struct {
	Data []ChannelData `json:"data"`
}

type ChannelData struct {
	ChannelID       string `json:"channelId"`
	ChannelName     string `json:"channelName"`
	ChannelImageURL string `json:"channelImageUrl"`
	FollowerCount   int    `json:"followerCount"`
	VerifiedMark    bool   `json:"verifiedMark"`
}
