export interface YouTubeCommunityShortsOpsOverview {
	channelCount: number;
	detectedPostCount: number;
	alarmSentPostCount: number;
	successPostCount: number;
	failedPostCount: number;
	detectedUnsentPostCount: number;
	pendingPostCount: number;
	latencyMeasuredPostCount: number;
	withinTargetPostCount: number;
	exceededPostCount: number;
	communityDetectedPostCount: number;
	shortsDetectedPostCount: number;
	communityExceededPostCount: number;
	shortsExceededPostCount: number;
	averageLatencyMillis?: number;
	maxLatencyMillis?: number;
}

export interface YouTubeCommunityShortsOpsChannel {
	channelId: string;
	memberName?: string;
	earliestObservedAt?: string;
	latestObservedAt?: string;
	detectedPostCount: number;
	alarmSentPostCount: number;
	successPostCount: number;
	failedPostCount: number;
	detectedUnsentPostCount: number;
	pendingPostCount: number;
	latencyMeasuredPostCount: number;
	withinTargetPostCount: number;
	exceededPostCount: number;
	communityPostCount: number;
	shortsPostCount: number;
	averageLatencyMillis?: number;
	maxLatencyMillis?: number;
}

export interface YouTubeCommunityShortsOpsResponse {
	status: string;
	generatedAt: string;
	windowStart: string;
	windowEnd: string;
	windowHours: number;
	observedAtBasis: string;
	slaThresholdMillis: number;
	overview: YouTubeCommunityShortsOpsOverview;
	channels: YouTubeCommunityShortsOpsChannel[];
}
