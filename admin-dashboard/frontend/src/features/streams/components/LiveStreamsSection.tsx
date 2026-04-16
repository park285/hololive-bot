import ChevronDown from "lucide-react/dist/esm/icons/chevron-down";
import ExternalLink from "lucide-react/dist/esm/icons/external-link";
import PlayCircle from "lucide-react/dist/esm/icons/play-circle";
import type { SyntheticEvent } from "react";
import { VirtualList } from "@/components/ui/VirtualList";
import {
	getStreamKey,
	getStreamLinkMeta,
	getThumbnailSource,
} from "@/features/streams/lib/media";
import type { Stream, StreamOrg } from "@/features/streams/types";

const LIVE_STREAM_ROW_SIZE = 3;

const chunkLiveStreams = (streams: Stream[]) => {
	const rows: Stream[][] = [];
	for (let index = 0; index < streams.length; index += LIVE_STREAM_ROW_SIZE) {
		rows.push(streams.slice(index, index + LIVE_STREAM_ROW_SIZE));
	}
	return rows;
};

interface OrgOption {
	value: StreamOrg;
	label: string;
}

interface LiveStreamsSectionProps {
	selectedOrg: StreamOrg;
	orgOptions: OrgOption[];
	liveStreams: Stream[];
	liveLoading: boolean;
	onOrgChange: (org: StreamOrg) => void;
	onThumbnailError: (event: SyntheticEvent<HTMLImageElement>) => void;
}

export const LiveStreamsSection = ({
	selectedOrg,
	orgOptions,
	liveStreams,
	liveLoading,
	onOrgChange,
	onThumbnailError,
}: LiveStreamsSectionProps) => (
	<div className="bg-white rounded-xl shadow-sm border border-slate-200 p-6">
		<div className="flex items-center justify-between gap-3 mb-4">
			<div className="flex items-center gap-2">
				<PlayCircle className="text-rose-500" />
				<h3 className="text-lg font-bold text-slate-800">Live Streams</h3>
				<span className="text-xs font-medium px-2 py-0.5 rounded-full bg-rose-100 text-rose-600">
					{liveStreams.length}
				</span>
			</div>

			<div className="relative">
				<select
					value={selectedOrg}
					onChange={(event) => {
						onOrgChange(event.target.value as StreamOrg);
					}}
					className="appearance-none bg-slate-50 border border-slate-200 text-slate-700 text-sm font-medium rounded-lg py-2 pl-3 pr-8 focus:outline-none focus:ring-2 focus:ring-sky-500 focus:border-transparent cursor-pointer hover:bg-slate-100 transition-colors"
					aria-label="스트림 org 선택"
				>
					{orgOptions.map((option) => (
						<option key={option.value} value={option.value}>
							{option.label}
						</option>
					))}
				</select>
				<ChevronDown
					className="absolute right-2.5 top-2.5 text-slate-400 pointer-events-none"
					size={16}
				/>
			</div>
		</div>

		{liveLoading ? (
			<div className="h-40 flex items-center justify-center text-slate-400 text-sm">
				Loading…
			</div>
		) : liveStreams.length === 0 ? (
			<p className="col-span-full text-center text-slate-400 text-sm py-10">
				No live streams currently.
			</p>
		) : (
			<VirtualList
				items={chunkLiveStreams(liveStreams)}
				estimateSize={() => 300}
				className="max-h-[42rem] pr-1"
				itemClassName="pb-4"
				renderItem={(row, rowIndex) => (
					<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
						{row.map((stream, columnIndex) => {
							const thumbnail = getThumbnailSource(
								stream.thumbnail ?? undefined,
								"max",
							);
							const linkMeta = getStreamLinkMeta(stream);
							const streamIndex = rowIndex * LIVE_STREAM_ROW_SIZE + columnIndex;

							return (
								<a
									key={getStreamKey(stream, streamIndex)}
									href={linkMeta.href}
									target="_blank"
									rel="noreferrer"
									className="group relative block rounded-xl overflow-hidden border border-slate-200 hover:shadow-md transition-shadow [content-visibility:auto] contain-intrinsic-size-[300px]"
								>
									{thumbnail ? (
										<div className="aspect-video relative overflow-hidden bg-slate-100">
											<img
												src={thumbnail.src}
												srcSet={thumbnail.srcSet}
												sizes={thumbnail.sizes}
												data-fallback-chain={thumbnail.fallbackChain.join("|")}
												alt={stream.title}
												loading={streamIndex === 0 ? "eager" : "lazy"}
												decoding="async"
												fetchPriority={streamIndex === 0 ? "high" : "auto"}
												className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
												onError={onThumbnailError}
											/>
											<div className="absolute top-2 right-2 bg-rose-600 text-white text-[10px] font-bold px-1.5 py-0.5 rounded flex items-center gap-1 shadow-sm">
												LIVE
											</div>
										</div>
									) : (
										<div className="aspect-video bg-slate-100 flex items-center justify-center text-slate-300">
											<PlayCircle size={32} />
										</div>
									)}
									<div className="p-4">
										<h4 className="font-bold text-sm line-clamp-2 mb-1 text-slate-800">
											{stream.title}
										</h4>
										<p className="text-xs text-slate-500 mb-3">
											{stream.channel_name}
										</p>
										<span className="inline-flex items-center text-xs font-medium text-red-600 group-hover:text-red-700 group-hover:underline">
											<ExternalLink size={12} className="mr-1" /> {linkMeta.label}
										</span>
									</div>
								</a>
							);
						})}
					</div>
				)}
			/>
		)}
	</div>
);
