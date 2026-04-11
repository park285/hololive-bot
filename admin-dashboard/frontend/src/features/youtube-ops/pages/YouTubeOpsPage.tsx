import { useQuery } from "@tanstack/react-query";
import {
	Activity,
	AlertTriangle,
	BellRing,
	CheckCircle2,
	Loader2,
	Radio,
	ShieldAlert,
	Signal,
	Timer,
} from "lucide-react";
import { queryKeys } from "@/api/queryKeys";
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

const toneStyles = {
	sky: "border-sky-200 bg-sky-50/80 text-sky-700",
	emerald: "border-emerald-200 bg-emerald-50/80 text-emerald-700",
	amber: "border-amber-200 bg-amber-50/80 text-amber-700",
	rose: "border-rose-200 bg-rose-50/80 text-rose-700",
	indigo: "border-indigo-200 bg-indigo-50/80 text-indigo-700",
	slate: "border-slate-200 bg-slate-50/80 text-slate-700",
} as const;

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

const resolveChannelStatus = (channel: YouTubeCommunityShortsOpsChannel) => {
	if (channel.detectedUnsentPostCount > 0 && channel.exceededPostCount > 0) {
		return {
			label: "미발송 + SLA 초과",
			badgeClass: toneStyles.rose,
			rowClass: "border-rose-200/80 bg-rose-50/40",
		};
	}
	if (channel.detectedUnsentPostCount > 0) {
		return {
			label: "미발송 추적 필요",
			badgeClass: toneStyles.amber,
			rowClass: "border-amber-200/80 bg-amber-50/40",
		};
	}
	if (channel.exceededPostCount > 0) {
		return {
			label: "SLA 초과 있음",
			badgeClass: toneStyles.indigo,
			rowClass: "border-indigo-200/80 bg-indigo-50/40",
		};
	}
	if (channel.failedPostCount > 0) {
		return {
			label: "실패 시도 존재",
			badgeClass: toneStyles.slate,
			rowClass: "border-slate-200/80 bg-slate-50/50",
		};
	}
	return {
		label: "정상",
		badgeClass: toneStyles.emerald,
		rowClass: "border-slate-200/70 bg-white/90",
	};
};

const OverviewCard = ({
	label,
	value,
	detail,
	icon,
	tone,
}: {
	label: string;
	value: string;
	detail: string;
	icon: React.ReactNode;
	tone: keyof typeof toneStyles;
}) => (
	<div className="rounded-3xl border border-slate-200 bg-white/85 p-5 shadow-sm shadow-slate-200/70 backdrop-blur-sm">
		<div className="flex items-start justify-between gap-4">
			<div>
				<p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
					{label}
				</p>
				<p className="mt-3 text-3xl font-display font-bold text-slate-900">
					{value}
				</p>
				<p className="mt-2 text-sm leading-6 text-slate-500">{detail}</p>
			</div>
			<div
				className={`flex h-11 w-11 items-center justify-center rounded-2xl border ${toneStyles[tone]}`}
			>
				{icon}
			</div>
		</div>
	</div>
);

const SectionCard = ({
	title,
	description,
	children,
}: {
	title: string;
	description: string;
	children: React.ReactNode;
}) => (
	<section className="rounded-[28px] border border-slate-200 bg-white/90 p-6 shadow-sm shadow-slate-200/70 backdrop-blur-sm">
		<div className="mb-5 flex flex-col gap-2">
			<h3 className="text-lg font-display font-bold text-slate-900">{title}</h3>
			<p className="text-sm leading-6 text-slate-500">{description}</p>
		</div>
		{children}
	</section>
);

const MetricDefinition = ({
	title,
	meaning,
	basis,
}: {
	title: string;
	meaning: string;
	basis: string;
}) => (
	<div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
		<p className="text-sm font-semibold text-slate-900">{title}</p>
		<p className="mt-2 text-sm leading-6 text-slate-600">{meaning}</p>
		<p className="mt-3 text-xs leading-5 text-slate-400">{basis}</p>
	</div>
);

const buildOverviewCards = (data: YouTubeCommunityShortsOpsResponse) => [
	{
		label: "채널 수",
		value: formatCount(data.overview.channelCount),
		detail: "최근 24시간 안에 커뮤니티/쇼츠 게시물이 관측된 채널 수",
		icon: <Signal size={20} />,
		tone: "sky" as const,
	},
	{
		label: "감지 게시물",
		value: formatCount(data.overview.detectedPostCount),
		detail: `커뮤니티 ${formatCount(data.overview.communityDetectedPostCount)} / 쇼츠 ${formatCount(data.overview.shortsDetectedPostCount)}`,
		icon: <Activity size={20} />,
		tone: "sky" as const,
	},
	{
		label: "성공 발송",
		value: formatCount(data.overview.successPostCount),
		detail: `알람 전송 활동 ${formatCount(data.overview.alarmSentPostCount)}건 중 canonical success 기준`,
		icon: <CheckCircle2 size={20} />,
		tone: "emerald" as const,
	},
	{
		label: "미발송 후보",
		value: formatCount(data.overview.detectedUnsentPostCount),
		detail: `pending ${formatCount(data.overview.pendingPostCount)}건, 실패 시도 ${formatCount(data.overview.failedPostCount)}건 포함`,
		icon: <BellRing size={20} />,
		tone: "amber" as const,
	},
	{
		label: "2분 초과",
		value: formatCount(data.overview.exceededPostCount),
		detail: `커뮤니티 ${formatCount(data.overview.communityExceededPostCount)} / 쇼츠 ${formatCount(data.overview.shortsExceededPostCount)}`,
		icon: <ShieldAlert size={20} />,
		tone: "rose" as const,
	},
	{
		label: "지연 분포",
		value: `${formatLatency(data.overview.averageLatencyMillis)} / ${formatLatency(data.overview.maxLatencyMillis)}`,
		detail: `평균 / 최대 지연, 측정 가능 게시물 ${formatCount(data.overview.latencyMeasuredPostCount)}건`,
		icon: <Timer size={20} />,
		tone: "indigo" as const,
	},
];

export const YouTubeOpsPage = () => {
	const query = useQuery({
		queryKey: queryKeys.youtubeOps.summary,
		queryFn: youtubeOpsApi.get,
		staleTime: 30000,
		refetchInterval: 60000,
	});

	if (query.isLoading) {
		return (
			<div className="flex h-64 items-center justify-center text-slate-400">
				<div className="mr-2 animate-spin">
					<Loader2 />
				</div>
				유튜브 운영 집계를 불러오는 중…
			</div>
		);
	}

	if (query.isError || !query.data) {
		return (
			<div className="rounded-3xl border border-rose-200 bg-rose-50/80 p-8 text-center text-rose-700 shadow-sm">
				<div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-2xl border border-rose-200 bg-white/90">
					<AlertTriangle size={22} />
				</div>
				<p className="text-lg font-semibold">유튜브 운영 집계를 불러오지 못했습니다</p>
				<p className="mt-2 text-sm text-rose-600/80">
					bot API 또는 관리자 프록시 응답을 확인해 주세요.
				</p>
			</div>
		);
	}

	const { data } = query;
	const overviewCards = buildOverviewCards(data);
	const attentionChannels = data.channels.filter(
		(channel) =>
			channel.detectedUnsentPostCount > 0 ||
			channel.exceededPostCount > 0 ||
			channel.failedPostCount > 0,
	).length;
	const metricDefinitions = [
		{
			title: "detectedPostCount",
			meaning:
				"24시간 창에 실제 게시 시각이 들어온 게시물 수입니다. 실제 게시 시각이 비어 있으면 detected_at fallback을 사용합니다.",
			basis:
				"기준 시각: COALESCE(actual_published_at, detected_at), 범위: [windowStart, windowEnd)",
		},
		{
			title: "successPostCount",
			meaning:
				"게시물당 최소 1회의 canonical success가 기록된 수입니다. 운영자가 1회 발송 보장 여부를 가장 먼저 볼 때 쓰는 값입니다.",
			basis:
				"판정 근거: alarm_sent_at 또는 success telemetry 존재 여부",
		},
		{
			title: "detectedUnsentPostCount",
			meaning:
				"감지는 됐지만 success가 아직 없는 게시물 수입니다. 늦더라도 1회 발송돼야 하는 미완료 후보입니다.",
			basis:
				"success_send_count == 0 이거나 canonical success 시각이 비어 있는 게시물",
		},
		{
			title: "pendingPostCount",
			meaning:
				"alarm_sent_at 자체가 비어 있는 게시물 수입니다. 내부 전달 완료 전 단계의 적체를 빠르게 확인할 때 씁니다.",
			basis:
				"alarm_sent_at IS NULL 기준, detectedUnsentPostCount보다 더 이른 경보 값",
		},
		{
			title: "exceededPostCount",
			meaning:
				"실제 게시 시각 기준 2분 초과가 확정된 게시물 수입니다. 운영 알림은 아니지만 원인 분석 우선순위를 정할 때 씁니다.",
			basis:
				`SLA 임계치 ${formatLatency(data.slaThresholdMillis)} 초과, alarm_latency_exceeded = true`,
		},
		{
			title: "averageLatencyMillis / maxLatencyMillis",
			meaning:
				"지연이 계산 가능한 게시물만 대상으로 한 평균/최대 지연입니다. 측정 불가 건은 제외됩니다.",
			basis:
				"latencyMeasuredPostCount 집합만 포함, 표기는 KST로 변환해 보여줌",
		},
	];

	return (
		<div className="space-y-8">
			<section className="relative overflow-hidden rounded-[32px] border border-slate-200 bg-white/90 p-7 shadow-sm shadow-slate-200/70 backdrop-blur-sm">
				<div className="absolute inset-x-0 top-0 h-24 bg-linear-to-r from-sky-100/80 via-cyan-50 to-amber-50" />
				<div className="relative flex flex-col gap-6 lg:flex-row lg:items-end lg:justify-between">
					<div className="max-w-3xl">
						<div className="inline-flex items-center gap-2 rounded-full border border-sky-200 bg-sky-50 px-3 py-1 text-xs font-semibold uppercase tracking-[0.18em] text-sky-700">
							<Radio size={14} />
							YouTube Ops
						</div>
						<h2 className="mt-4 text-3xl font-display font-bold tracking-tight text-slate-900">
							커뮤니티 / 쇼츠 24시간 운영 집계
						</h2>
						<p className="mt-3 max-w-2xl text-sm leading-7 text-slate-600">
							실제 게시 시각 기준으로 최근 24시간의 감지, 발송, 미발송, 2분 초과를 채널별로 한 화면에서 확인합니다.
							 다른 알람 유형과 UI는 건드리지 않고 community / shorts만 분리 모니터링합니다.
						</p>
					</div>
					<div className="grid gap-3 text-sm text-slate-500 sm:grid-cols-2 lg:min-w-[360px]">
						<div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4">
							<p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
								조회 구간
							</p>
							<p className="mt-2 font-semibold text-slate-900">
								{formatTimestamp(data.windowStart)}
							</p>
							<p className="text-slate-400">~ {formatTimestamp(data.windowEnd)}</p>
						</div>
						<div className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4">
							<p className="text-xs font-semibold uppercase tracking-[0.18em] text-slate-400">
								조회 기준
							</p>
							<p className="mt-2 font-mono text-xs text-slate-700">
								{data.observedAtBasis}
							</p>
							<p className="mt-2 text-xs text-slate-400">
								표시 시간대는 KST, SLA 임계치는 {formatLatency(data.slaThresholdMillis)} 입니다.
							</p>
						</div>
					</div>
				</div>
			</section>

			<div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
				{overviewCards.map((card) => (
					<OverviewCard key={card.label} {...card} />
				))}
			</div>

			<div className="grid gap-6 xl:grid-cols-[1.1fr_1.4fr]">
				<SectionCard
					title="운영 해석"
					description="운영자가 화면에서 바로 읽어야 하는 기준만 짧게 정리했습니다. 세부 해설은 runbook에 동일 기준으로 문서화합니다."
				>
					<div className="space-y-4 text-sm leading-7 text-slate-600">
						<div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
							<p className="font-semibold text-slate-900">주의 채널</p>
							<p className="mt-1">
								{formatCount(attentionChannels)}개 채널에 미발송, 실패 시도, SLA 초과가 하나 이상 있습니다.
							</p>
						</div>
						<div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
							<p className="font-semibold text-slate-900">누락 / 중복 우선 확인</p>
							<p className="mt-1">
								`detectedUnsentPostCount`와 `successPostCount`를 먼저 보고, 이후 `failedPostCount`와 `pendingPostCount`로 내부 원인 범위를 줄입니다.
							</p>
						</div>
						<div className="rounded-2xl border border-slate-200 bg-slate-50/70 p-4">
							<p className="font-semibold text-slate-900">지연 해석</p>
							<p className="mt-1">
								`exceededPostCount`는 2분 초과 확정 건이며, 평균/최대 지연은 측정 가능한 게시물만 기준으로 계산됩니다.
							</p>
						</div>
					</div>
				</SectionCard>

				<SectionCard
					title="지표 의미"
					description="화면에 노출한 값의 의미와 집계 기준을 명시적으로 붙여 운영 판정이 흔들리지 않게 합니다."
				>
					<div className="grid gap-4 md:grid-cols-2">
						{metricDefinitions.map((definition) => (
							<MetricDefinition key={definition.title} {...definition} />
						))}
					</div>
				</SectionCard>
			</div>

			<SectionCard
				title="채널별 24시간 집계"
				description="행 강조는 미발송, 실패 시도, 2분 초과 여부를 기준으로 정합니다. 표 정렬은 최신 관측 시각 우선입니다."
			>
				{data.channels.length === 0 ? (
					<div className="rounded-2xl border border-dashed border-slate-200 bg-slate-50/70 p-10 text-center text-slate-500">
						최근 24시간에 community / shorts 게시물이 감지된 채널이 없습니다.
					</div>
				) : (
					<div className="overflow-x-auto">
						<table className="min-w-full border-separate border-spacing-y-3 text-sm text-slate-600">
							<thead>
								<tr className="text-left text-xs uppercase tracking-[0.18em] text-slate-400">
									<th className="px-4 py-2">채널</th>
									<th className="px-4 py-2">최신 관측</th>
									<th className="px-4 py-2">감지</th>
									<th className="px-4 py-2">성공</th>
									<th className="px-4 py-2">미발송</th>
									<th className="px-4 py-2">pending</th>
									<th className="px-4 py-2">2분 초과</th>
									<th className="px-4 py-2">평균 / 최대</th>
								</tr>
							</thead>
							<tbody>
								{data.channels.map((channel) => {
									const status = resolveChannelStatus(channel);
									return (
										<tr key={channel.channelId}>
											<td className="px-0 py-0">
												<div className={`rounded-2xl border px-4 py-4 ${status.rowClass}`}>
													<div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
														<div>
															<p className="font-semibold text-slate-900">
																{channel.memberName || channel.channelId}
															</p>
															<p className="mt-1 font-mono text-xs text-slate-400">{channel.channelId}</p>
															<p className="mt-2 text-xs text-slate-500">
																community {formatCount(channel.communityPostCount)} / shorts {formatCount(channel.shortsPostCount)}
															</p>
														</div>
														<div className={`inline-flex items-center rounded-full border px-3 py-1 text-xs font-semibold ${status.badgeClass}`}>
															{status.label}
														</div>
													</div>
												</div>
											</td>
											<td className="px-4 py-4 align-top text-slate-700">
												{formatTimestamp(channel.latestObservedAt)}
											</td>
											<td className="px-4 py-4 align-top font-semibold text-slate-900">
												{formatCount(channel.detectedPostCount)}
											</td>
											<td className="px-4 py-4 align-top font-semibold text-emerald-700">
												{formatCount(channel.successPostCount)}
											</td>
											<td className="px-4 py-4 align-top font-semibold text-amber-700">
												{formatCount(channel.detectedUnsentPostCount)}
											</td>
											<td className="px-4 py-4 align-top font-semibold text-slate-700">
												{formatCount(channel.pendingPostCount)}
											</td>
											<td className="px-4 py-4 align-top font-semibold text-rose-700">
												{formatCount(channel.exceededPostCount)}
											</td>
											<td className="px-4 py-4 align-top text-slate-700">
												<div>{formatLatency(channel.averageLatencyMillis)}</div>
												<div className="text-xs text-slate-400">max {formatLatency(channel.maxLatencyMillis)}</div>
											</td>
										</tr>
									);
								})}
							</tbody>
						</table>
					</div>
				)}
			</SectionCard>
		</div>
	);
};
