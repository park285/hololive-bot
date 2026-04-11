import { useQuery } from "@tanstack/react-query";
import {
	Activity,
	AlertTriangle,
	BellRing,
	CheckCircle2,
	Loader2,
	Play,
	ShieldAlert,
	Signal,
	Timer,
} from "lucide-react";
import { queryKeys } from "@/api/queryKeys";
import { alarmsApi } from "@/features/alarms/api";
import { Badge } from "@/components/ui/Badge";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/Card";
import { StatCard } from "@/components/ui/StatCard";
import { youtubeOpsApi } from "@/features/youtube-ops/api";
import type {
	YouTubeCommunityShortsOpsChannel,
	YouTubeCommunityShortsOpsResponse,
} from "@/features/youtube-ops/types";

const numberFormatter = new Intl.NumberFormat("ko-KR");
const timestampFormatter = new Intl.DateTimeFormat("ko-KR", {
	timeZone: "Asia/Seoul",
	month: "2-digit",
	day: "2-digit",
	hour: "2-digit",
	minute: "2-digit",
	hour12: false,
});

const formatCount = (value: number) => numberFormatter.format(value);

const formatLatency = (value?: number) => {
	if (value == null) {
		return "-";
	}
	if (value < 1000) {
		return `${value.toString()}ms`;
	}
	return `${(value / 1000).toFixed(value % 1000 === 0 ? 0 : 1)}초`;
};

const formatTimestamp = (value?: string) => {
	if (!value) {
		return "-";
	}
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) {
		return value;
	}
	return `${timestampFormatter.format(date)} KST`;
};

const resolveChannelStatus = (
	channel: YouTubeCommunityShortsOpsChannel,
	hasAlarm: boolean,
) => {
	if (!hasAlarm) {
		return {
			label: "알람 미설정",
			variant: "secondary" as const,
		};
	}

	if (channel.detectedUnsentPostCount > 0 && channel.exceededPostCount > 0) {
		return {
			label: "미발송 + 지연",
			variant: "rose" as const,
		};
	}
	if (channel.detectedUnsentPostCount > 0) {
		return {
			label: "미발송 (추적 필요)",
			variant: "amber" as const,
		};
	}
	if (channel.exceededPostCount > 0) {
		return {
			label: "발송 지연",
			variant: "indigo" as const,
		};
	}
	if (channel.failedPostCount > 0) {
		return {
			label: "발송 실패 존재",
			variant: "gray" as const,
		};
	}
	return {
		label: "정상",
		variant: "green" as const,
	};
};

const MetricDefinition = ({
	title,
	meaning,
	basis,
}: {
	title: string;
	meaning: string;
	basis: string;
}) => (
	<div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
		<p className="text-sm font-semibold text-slate-800">{title}</p>
		<p className="mt-2 text-sm leading-relaxed text-slate-600">{meaning}</p>
		<p className="mt-3 text-xs leading-relaxed text-slate-500">{basis}</p>
	</div>
);

const buildOverviewCards = (data: YouTubeCommunityShortsOpsResponse) => [
	{
		label: "채널 수",
		value: formatCount(data.overview.channelCount),
		detail: "최근 24시간 커뮤니티/쇼츠 감지",
		icon: <Signal size={20} />,
		variant: "blue" as const,
	},
	{
		label: "감지 게시물",
		value: formatCount(data.overview.detectedPostCount),
		detail: `커뮤니티 ${formatCount(data.overview.communityDetectedPostCount)} / 쇼츠 ${formatCount(data.overview.shortsDetectedPostCount)}`,
		icon: <Activity size={20} />,
		variant: "cyan" as const,
	},
	{
		label: "성공 발송",
		value: formatCount(data.overview.successPostCount),
		detail: `${formatCount(data.overview.alarmSentPostCount)}건 중 canonical success`,
		icon: <CheckCircle2 size={20} />,
		variant: "green" as const,
	},
	{
		label: "미발송 후보",
		value: formatCount(data.overview.detectedUnsentPostCount),
		detail: `pending ${formatCount(data.overview.pendingPostCount)} / 실패 ${formatCount(data.overview.failedPostCount)}`,
		icon: <BellRing size={20} />,
		variant: "yellow" as const,
	},
	{
		label: "2분 초과",
		value: formatCount(data.overview.exceededPostCount),
		detail: `커뮤니티 ${formatCount(data.overview.communityExceededPostCount)} / 쇼츠 ${formatCount(data.overview.shortsExceededPostCount)}`,
		icon: <ShieldAlert size={20} />,
		variant: "rose" as const,
	},
	{
		label: "지연 (평균/최대)",
		value: `${formatLatency(data.overview.averageLatencyMillis)} / ${formatLatency(data.overview.maxLatencyMillis)}`,
		detail: `측정 가능 게시물 ${formatCount(data.overview.latencyMeasuredPostCount)}건 기준`,
		icon: <Timer size={20} />,
		variant: "indigo" as const,
	},
];

export const YouTubeOpsPage = () => {
	const query = useQuery({
		queryKey: queryKeys.youtubeOps.summary,
		queryFn: youtubeOpsApi.get,
		staleTime: 30000,
		refetchInterval: 60000,
	});

	const alarmsQuery = useQuery({
		queryKey: queryKeys.alarms.all,
		queryFn: alarmsApi.getAll,
		staleTime: 60000,
	});

	if (query.isLoading || alarmsQuery.isLoading) {
		return (
			<div className="flex justify-center items-center h-64 text-slate-400">
				<div className="animate-spin mr-2">
					<Loader2 />
				</div>
				유튜브 운영 집계 및 알람 정보를 불러오는 중…
			</div>
		);
	}

	if (query.isError || !query.data) {
		return (
			<div className="text-center py-12 bg-rose-50 rounded-2xl border border-rose-100">
				<div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-xl bg-rose-100 text-rose-600">
					<AlertTriangle size={24} />
				</div>
				<p className="text-lg font-bold text-rose-700">
					유튜브 운영 집계를 불러오지 못했습니다
				</p>
				<p className="mt-2 text-sm text-rose-600/80">
					bot API 또는 관리자 프록시 응답을 확인해 주세요.
				</p>
			</div>
		);
	}

	const { data } = query;
	const overviewCards = buildOverviewCards(data);

	const alarmedChannelIds = new Set(
		alarmsQuery.data?.alarms.map((a) => a.channelId) ?? [],
	);

	const attentionChannels = data.channels.filter(
		(channel) =>
			alarmedChannelIds.has(channel.channelId) &&
			(channel.detectedUnsentPostCount > 0 ||
				channel.exceededPostCount > 0 ||
				channel.failedPostCount > 0),
	).length;

	const metricDefinitions = [
		{
			title: "detectedPostCount",
			meaning:
				"최근 24시간 동안 감지된 전체 게시물 수입니다. 실제 게시 시각(actual_published_at)을 기준으로 하며, 없을 경우 감지 시각(detected_at)을 대체재로 사용합니다.",
			basis:
				"조회 기준: COALESCE(actual_published_at, detected_at), 대상 범위: [windowStart, windowEnd)",
		},
		{
			title: "successPostCount",
			meaning:
				"게시물당 최소 1회 이상 정상적으로 발송(canonical success)이 완료된 수입니다. 사용자에게 정상적으로 전달되었는지를 판단하는 기준입니다.",
			basis: "판정 기준: alarm_sent_at 존재 여부 또는 success telemetry 기록",
		},
		{
			title: "detectedUnsentPostCount",
			meaning:
				"게시물이 감지되었으나 아직 발송 성공 기록이 없는 수입니다. 알람이 설정되어 있다면 발송 누락 가능성이 있으므로 추적이 필요합니다.",
			basis:
				"판정 기준: success_send_count가 0이거나 canonical success 시각이 없는 경우",
		},
		{
			title: "pendingPostCount",
			meaning:
				"내부 전달이 아직 완료되지 않아 alarm_sent_at 값이 없는 게시물 수입니다. 큐(Queue) 적체 등 내부 처리 지연을 파악할 때 확인합니다.",
			basis:
				"판정 기준: alarm_sent_at IS NULL (detectedUnsentPostCount에 포함됨)",
		},
		{
			title: "exceededPostCount",
			meaning:
				"실제 게시 시각을 기준으로 알람 발송까지 허용 시간(SLA)을 초과한 게시물 수입니다. 발송은 완료되었으나 성능 개선이 필요한 경우입니다.",
			basis: `판정 기준: SLA 임계치 ${formatLatency(data.slaThresholdMillis)} 초과 및 alarm_latency_exceeded = true`,
		},
		{
			title: "averageLatencyMillis / maxLatencyMillis",
			meaning:
				"지연 시간을 측정할 수 있는 게시물들의 평균 및 최대 발송 소요 시간입니다. 측정 불가능한 게시물은 통계에서 제외됩니다.",
			basis:
				"통계 대상: latencyMeasuredPostCount에 포함된 게시물 (KST 기준 변환 표시)",
		},
	];

	return (
		<div className="space-y-8">
			{/* Hero Section */}
			<div className="relative overflow-hidden rounded-3xl bg-white border border-slate-100 p-8 shadow-sm animate-fade-in-up">
				<div className="absolute top-0 right-0 w-96 h-96 bg-red-50 rounded-full blur-3xl opacity-60 -mr-20 -mt-20 pointer-events-none" />
				<div className="absolute bottom-0 left-0 w-64 h-64 bg-rose-50 rounded-full blur-3xl opacity-40 -ml-10 -mb-10 pointer-events-none" />

				<div className="relative z-10 flex flex-col md:flex-row items-center justify-between gap-8">
					<div className="max-w-2xl">
						<div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-rose-50 border border-rose-100 text-rose-600 text-xs font-semibold mb-4">
							<span className="relative flex h-2 w-2">
								<span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-rose-400 opacity-75" />
								<span className="relative inline-flex rounded-full h-2 w-2 bg-rose-500" />
							</span>
							YouTube Ops
						</div>
						<h1 className="text-3xl font-display font-bold text-slate-800 tracking-tight">
							YouTube 커뮤니티 및 쇼츠 운영 현황
						</h1>
						<p className="mt-3 text-slate-500 max-w-xl">
							최근 24시간 동안 감지된 YouTube 커뮤니티 및 쇼츠 게시물의 알람 발송 현황을 모니터링합니다. 알람이 설정된 채널의 미발송 및 발송 지연(2분 초과) 상태를 중점적으로 확인합니다.
						</p>
						<div className="mt-6 flex flex-wrap gap-4">
							<div className="rounded-xl border border-slate-100 bg-slate-50/50 p-3 px-4">
								<p className="text-xs font-semibold text-slate-500 uppercase">조회 구간</p>
								<p className="mt-1 font-medium text-slate-800 text-sm">
									{formatTimestamp(data.windowStart)} ~ {formatTimestamp(data.windowEnd)}
								</p>
							</div>
							<div className="rounded-xl border border-slate-100 bg-slate-50/50 p-3 px-4">
								<p className="text-xs font-semibold text-slate-500 uppercase">조회 기준</p>
								<p className="mt-1 font-mono text-xs text-slate-700">
									{data.observedAtBasis}
								</p>
							</div>
						</div>
					</div>

					<div className="hidden md:flex items-center justify-center w-32 h-32 bg-linear-to-br from-rose-400 via-red-400 to-orange-400 rounded-3xl shadow-xl shadow-rose-200/60 transform -rotate-6 border-4 border-white hover:-rotate-3 transition-transform duration-500">
						<Play className="w-16 h-16 text-white drop-shadow-md fill-white ml-2" />
					</div>
				</div>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6">
				{overviewCards.map((card, idx) => (
					<div
						key={card.label}
						className="animate-fade-in-up"
						style={{ animationDelay: `${String((idx + 1) * 80)}ms` }}
					>
						<StatCard
							label={card.label}
							value={card.value}
							icon={card.icon}
							variant={card.variant}
						/>
						<p className="text-xs text-slate-500 mt-2 px-1 text-center">{card.detail}</p>
					</div>
				))}
			</div>

			<div className="grid gap-6 xl:grid-cols-[1.1fr_1.4fr]">
				<Card>
					<CardHeader>
						<CardTitle>점검 필요 현황</CardTitle>
						<CardDescription>
							운영자가 신속하게 파악해야 하는 모니터링 기준 안내
						</CardDescription>
					</CardHeader>
					<CardContent className="space-y-4">
						<div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
							<p className="font-semibold text-slate-800 text-sm">점검 필요 채널</p>
							<p className="mt-1 text-sm text-slate-600">
								<span className="font-bold text-rose-600">{formatCount(attentionChannels)}</span>개의 알람 설정 채널에서 미발송, 실패 시도 또는 발송 지연이 발생했습니다.
							</p>
						</div>
						<div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
							<p className="font-semibold text-slate-800 text-sm">미발송 및 실패 원인 분석</p>
							<p className="mt-1 text-sm text-slate-600">
								`detectedUnsentPostCount`와 `successPostCount`를 먼저 확인한 후, `failedPostCount`와 `pendingPostCount`를 대조하여 내부 시스템 원인을 추적합니다.
							</p>
						</div>
						<div className="rounded-xl border border-slate-100 bg-slate-50 p-4">
							<p className="font-semibold text-slate-800 text-sm">발송 지연 기준</p>
							<p className="mt-1 text-sm text-slate-600">
								`exceededPostCount`는 지연이 확정된 건이며, 평균/최대 지연 시간은 발송 완료로 측정이 가능한 게시물만을 대상으로 계산됩니다.
							</p>
						</div>
					</CardContent>
				</Card>

				<Card>
					<CardHeader>
						<CardTitle>데이터 집계 기준</CardTitle>
						<CardDescription>
							화면에 표시되는 주요 지표들의 구체적인 산출 및 판별 기준
						</CardDescription>
					</CardHeader>
					<CardContent>
						<div className="grid gap-4 md:grid-cols-2">
							{metricDefinitions.map((definition) => (
								<MetricDefinition key={definition.title} {...definition} />
							))}
						</div>
					</CardContent>
				</Card>
			</div>

			<Card>
				<CardHeader>
					<CardTitle>채널별 최근 24시간 집계</CardTitle>
					<CardDescription>
						최근 관측 시각을 기준으로 정렬됩니다. 알람이 설정된 채널의 이상 상태(미발송, 실패, 지연)를 강조 표시합니다.
					</CardDescription>
				</CardHeader>
				<CardContent>
					{data.channels.length === 0 ? (
						<div className="rounded-2xl border border-dashed border-slate-200 bg-slate-50 p-12 text-center text-slate-500">
							최근 24시간 내에 감지된 커뮤니티 및 쇼츠 게시물이 없습니다.
						</div>
					) : (
						<div className="overflow-x-auto rounded-lg border border-slate-100 shadow-sm">
							<table className="w-full text-sm text-left">
								<thead className="text-xs text-slate-500 uppercase bg-slate-50 border-b border-slate-200">
									<tr>
										<th className="px-4 py-3.5 font-semibold">채널</th>
										<th className="px-4 py-3.5 font-semibold">상태</th>
										<th className="px-4 py-3.5 font-semibold text-right">감지</th>
										<th className="px-4 py-3.5 font-semibold text-right">성공</th>
										<th className="px-4 py-3.5 font-semibold text-right">미발송</th>
										<th className="px-4 py-3.5 font-semibold text-right">pending</th>
										<th className="px-4 py-3.5 font-semibold text-right">지연</th>
										<th className="px-4 py-3.5 font-semibold text-right">평균 / 최대 지연</th>
										<th className="px-4 py-3.5 font-semibold text-right">최근 관측</th>
									</tr>
								</thead>
								<tbody className="divide-y divide-slate-100">
									{data.channels.map((channel) => {
										const hasAlarm = alarmedChannelIds.has(channel.channelId);
										const status = resolveChannelStatus(channel, hasAlarm);
										return (
											<tr key={channel.channelId} className={`bg-white hover:bg-slate-50/50 transition-colors ${hasAlarm && status.variant !== "green" ? "bg-rose-50/30 hover:bg-rose-50/50" : ""}`}>
												<td className="px-4 py-3">
													<div className="font-medium text-slate-900">
														{channel.memberName || channel.channelId}
													</div>
													<div className="font-mono text-[10px] text-slate-400 mt-0.5">
														{channel.channelId}
													</div>
													<div className="text-[11px] text-slate-500 mt-1">
														C: {formatCount(channel.communityPostCount)} / S: {formatCount(channel.shortsPostCount)}
													</div>
												</td>
												<td className="px-4 py-3">
													<Badge variant={status.variant}>{status.label}</Badge>
												</td>
												<td className="px-4 py-3 text-right font-medium text-slate-800 tabular-nums">
													{formatCount(channel.detectedPostCount)}
												</td>
												<td className="px-4 py-3 text-right font-medium text-emerald-600 tabular-nums">
													{formatCount(channel.successPostCount)}
												</td>
												<td className="px-4 py-3 text-right font-medium text-amber-600 tabular-nums">
													{formatCount(channel.detectedUnsentPostCount)}
												</td>
												<td className="px-4 py-3 text-right font-medium text-slate-600 tabular-nums">
													{formatCount(channel.pendingPostCount)}
												</td>
												<td className="px-4 py-3 text-right font-medium text-rose-600 tabular-nums">
													{formatCount(channel.exceededPostCount)}
												</td>
												<td className="px-4 py-3 text-right text-slate-600 tabular-nums">
													{formatLatency(channel.averageLatencyMillis)}
													<span className="text-xs text-slate-400 block mt-0.5">
														max {formatLatency(channel.maxLatencyMillis)}
													</span>
												</td>
												<td className="px-4 py-3 text-right text-slate-500 whitespace-nowrap text-xs">
													{formatTimestamp(channel.latestObservedAt)}
												</td>
											</tr>
										);
									})}
								</tbody>
							</table>
						</div>
					)}
				</CardContent>
			</Card>
		</div>
	);
};
