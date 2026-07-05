import { useQuery } from "@tanstack/react-query";
import { cn } from "@/lib/utils";
import { queryKeys } from "@/api/queryKeys";
import { visibleRefetchInterval } from "@/lib/polling";
import { Skeleton } from "@/components/ui/Skeleton";
import { statsApi } from "@/features/stats/api";
import { selectTopChannelStats } from "@/features/stats/selectors";

const numberFormatter = new Intl.NumberFormat("ko-KR");

// 상위 3개 채널 순위 메달 색상 (금/은/동)
const RANK_STYLES = [
	"bg-linear-to-br from-amber-300 to-amber-500 text-white shadow-sm shadow-amber-200/50",
	"bg-linear-to-br from-slate-300 to-slate-400 text-white shadow-sm shadow-slate-200/50",
	"bg-linear-to-br from-orange-300 to-orange-500 text-white shadow-sm shadow-orange-200/50",
];

export const ChannelStatsTable = () => {
	const {
		data: topStats,
		isLoading,
		isError,
		error,
	} = useQuery({
		queryKey: queryKeys.stats.channels,
		queryFn: statsApi.getChannels,
		refetchInterval: visibleRefetchInterval(60000),
		select: selectTopChannelStats,
	});

	if (isLoading) {
		return (
			<div
				className="space-y-4"
				aria-busy="true"
				aria-label="채널 통계 로딩 중…"
			>
				<div className="rounded-2xl border border-border-subtle overflow-hidden">
					<div className="bg-muted h-10 w-full mb-1" />
					{Array.from({ length: 5 }, (_, i) => (
						<div key={i} className="flex gap-4 p-4 border-b border-slate-50 dark:border-slate-800">
							<Skeleton className="h-4 w-8" />
							<Skeleton className="h-4 flex-1" />
							<Skeleton className="h-4 w-20" />
							<Skeleton className="h-4 w-20" />
							<Skeleton className="h-4 w-12" />
						</div>
					))}
				</div>
			</div>
		);
	}

	if (isError) {
		return (
			<div
				role="alert"
				className="text-center text-rose-500 py-8 bg-rose-50 rounded-2xl border border-rose-100"
			>
				<p className="font-medium">채널 통계를 불러올 수 없습니다</p>
				<p className="text-xs text-rose-400 mt-1">
					{error instanceof Error
						? error.message
						: "알 수 없는 오류가 발생했습니다"}
				</p>
			</div>
		);
	}

	if (!topStats || topStats.length === 0) {
		return (
			<div className="text-center text-subtle-foreground py-12 bg-card rounded-2xl border border-border-subtle">
				표시할 채널 통계가 없습니다
			</div>
		);
	}

	return (
		<div className="rounded-2xl border border-border-subtle shadow-sm overflow-hidden">
			<div className="overflow-x-auto">
				<table
					className="w-full text-sm text-left"
					aria-label="상위 10개 채널 통계"
				>
					<thead className="text-xs text-muted-foreground uppercase bg-muted border-b border-border">
						<tr>
							<th
								scope="col"
								className="px-4 py-3.5 font-semibold w-12 text-center"
							>
								#
							</th>
							<th scope="col" className="px-4 py-3.5 font-semibold">
								채널명
							</th>
							<th scope="col" className="px-4 py-3.5 font-semibold text-right">
								구독자 수
							</th>
							<th scope="col" className="px-4 py-3.5 font-semibold text-right">
								총 조회수
							</th>
							<th scope="col" className="px-4 py-3.5 font-semibold text-right">
								동영상 수
							</th>
						</tr>
					</thead>
					<tbody className="divide-y divide-border-subtle">
						{topStats.map((stat, idx) => (
							<tr
								key={stat.ChannelID}
								className="bg-card hover:bg-linear-to-r hover:from-sky-50/50 hover:to-transparent transition-colors duration-200 group"
							>
								<td className="px-4 py-4 text-center">
									{idx < 3 ? (
										<span
											className={cn(
												"inline-flex items-center justify-center w-7 h-7 rounded-full text-xs font-bold",
												RANK_STYLES[idx],
											)}
										>
											{idx + 1}
										</span>
									) : (
										<span className="text-subtle-foreground font-bold">
											{idx + 1}
										</span>
									)}
								</td>
								<td className="px-4 py-4 font-medium text-foreground group-hover:text-sky-700 transition-colors">
									{stat.ChannelTitle}
								</td>
								<td className="px-4 py-4 text-right text-foreground font-medium tabular-nums font-mono">
									{numberFormatter.format(stat.SubscriberCount)}
								</td>
								<td className="px-4 py-4 text-right text-muted-foreground tabular-nums font-mono">
									{numberFormatter.format(stat.ViewCount)}
								</td>
								<td className="px-4 py-4 text-right text-muted-foreground tabular-nums font-mono">
									{numberFormatter.format(stat.VideoCount)}
								</td>
							</tr>
						))}
					</tbody>
				</table>
			</div>
		</div>
	);
};
