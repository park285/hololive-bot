import Calendar from "lucide-react/dist/esm/icons/calendar.mjs";
import ExternalLink from "lucide-react/dist/esm/icons/external-link.mjs";
import PlayCircle from "lucide-react/dist/esm/icons/play-circle.mjs";
import { type SyntheticEvent, useMemo } from "react";
import { QuerySection } from "@/components/ui/QuerySection";
import { VirtualList } from "@/components/ui/VirtualList";
import {
	getStreamKey,
	getStreamLinkMeta,
	getThumbnailSource,
} from "@/features/streams/lib/media";
import type { Stream } from "@/features/streams/types";
import type { SectionStateProps } from "@/lib/queryState";

const UPCOMING_ROW_SIZE = 2;

const chunkUpcomingStreams = (streams: Stream[]) => {
	const rows: Stream[][] = [];
	for (let index = 0; index < streams.length; index += UPCOMING_ROW_SIZE) {
		rows.push(streams.slice(index, index + UPCOMING_ROW_SIZE));
	}
	return rows;
};

const formatScheduledTime = (value: string | null | undefined) => {
	if (!value) {
		return "TBA";
	}

	const date = new Date(value);
	if (Number.isNaN(date.getTime())) {
		return "TBA";
	}

	return date.toLocaleTimeString("ko-KR", {
		hour: "2-digit",
		minute: "2-digit",
	});
};

interface UpcomingStreamsSectionProps {
	upcomingStreams: Stream[];
	state: SectionStateProps;
	onThumbnailError: (event: SyntheticEvent<HTMLImageElement>) => void;
}

export const UpcomingStreamsSection = ({
	upcomingStreams,
	state,
	onThumbnailError,
}: UpcomingStreamsSectionProps) => {
	const streamRows = useMemo(
		() => chunkUpcomingStreams(upcomingStreams),
		[upcomingStreams],
	);

	return (
		<div className="relative bg-card rounded-2xl shadow-sm border border-border p-6 overflow-hidden">
			<div className="flex items-center gap-2 mb-4">
			<div className="absolute top-0 left-0 right-0 h-1 bg-linear-to-r from-sky-400 to-cyan-400" />
				<span className="flex items-center justify-center w-8 h-8 rounded-lg bg-linear-to-br from-sky-400 to-cyan-400 text-white shadow-sm shadow-sky-200/50"><Calendar size={16} /></span>
				<h3 className="text-lg font-bold text-foreground">
					Upcoming Streams (24h)
				</h3>
				<span className="text-xs font-medium px-2 py-0.5 rounded-full bg-linear-to-r from-sky-400 to-cyan-400 text-white">
					{upcomingStreams.length}
				</span>
			</div>

			<QuerySection
				{...state}
				skeleton={
					<div className="h-40 flex items-center justify-center text-subtle-foreground text-sm">
						Loading…
					</div>
				}
				isEmpty={upcomingStreams.length === 0}
				emptyContent={
					<p className="col-span-full text-center text-subtle-foreground text-sm py-10">
						No upcoming streams found.
					</p>
				}
			>
				<VirtualList
					items={streamRows}
					estimateSize={() => 94}
					getItemKey={(row, rowIndex) =>
						row
							.map((stream, columnIndex) =>
								getStreamKey(stream, rowIndex * UPCOMING_ROW_SIZE + columnIndex),
							)
							.join("|") || `upcoming-row-${String(rowIndex)}`
					}
					className="max-h-[36rem] pr-1"
					itemClassName="pb-3"
					renderItem={(row, rowIndex) => (
						<div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
							{row.map((stream, columnIndex) => {
								const thumbnail = getThumbnailSource(
									stream.thumbnail ?? undefined,
									"sd",
								);
								const linkMeta = getStreamLinkMeta(stream);
								const streamIndex = rowIndex * UPCOMING_ROW_SIZE + columnIndex;

								return (
									<a
										key={getStreamKey(stream, streamIndex)}
										href={linkMeta.href}
										target="_blank"
										rel="noopener noreferrer"
										className="flex items-center p-3 rounded-lg border border-border-subtle hover:bg-accent transition-colors group [content-visibility:auto] contain-intrinsic-size-[80px]"
									>
										<div className="w-20 h-14 rounded-lg overflow-hidden shrink-0 bg-muted mr-4 relative flex items-center justify-center text-subtle-foreground">
											{thumbnail ? (
												<img
													src={thumbnail.src}
													srcSet={thumbnail.srcSet}
													sizes={thumbnail.sizes}
													data-fallback-chain={thumbnail.fallbackChain.join("|")}
													alt={stream.title}
													loading="lazy"
													decoding="async"
													className="w-full h-full object-cover"
													onError={onThumbnailError}
												/>
											) : (
												<PlayCircle size={20} />
											)}
										</div>
										<div className="flex-1 min-w-0">
											<h4 className="font-medium text-sm text-foreground truncate group-hover:text-sky-600 transition-colors">
												{stream.title}
											</h4>
											<p className="text-xs text-muted-foreground mt-0.5">
												{stream.channel_name}
											</p>
										</div>
										<div className="ml-4 text-right shrink-0 flex flex-col items-end gap-1">
											<div className="text-xs font-bold text-foreground bg-muted px-2 py-1 rounded whitespace-nowrap">
												{formatScheduledTime(stream.start_scheduled)}
											</div>
											<span className="inline-flex items-center gap-1 text-[10px] text-red-600 hover:text-red-700 hover:bg-red-50 px-2 py-0.5 rounded transition-colors">
												{linkMeta.badge}
												<ExternalLink size={10} />
											</span>
										</div>
									</a>
								);
							})}
						</div>
					)}
				/>
			</QuerySection>
		</div>
	);
};
