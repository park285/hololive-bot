import ChevronDown from "lucide-react/dist/esm/icons/chevron-down";
import ExternalLink from "lucide-react/dist/esm/icons/external-link";
import PlayCircle from "lucide-react/dist/esm/icons/play-circle";
import type { SyntheticEvent } from "react";
import {
	getStreamKey,
	getStreamLinkMeta,
	getThumbnailSource,
} from "@/features/streams/lib/media";
import type { Stream, StreamOrg } from "@/features/streams/types";

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
	<div className="bg-white rounded-2xl shadow-sm border border-slate-200/60 p-6 md:p-8">
		<div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 mb-6">
			<div className="flex items-center gap-3">
				<div className="flex items-center justify-center w-10 h-10 rounded-full bg-rose-50 text-rose-500 ring-4 ring-rose-50/50">
					<PlayCircle size={22} className="ml-0.5" />
				</div>
				<div>
					<h3 className="text-xl font-bold text-slate-900 tracking-tight">Live Streams</h3>
					<p className="text-sm text-slate-500 font-medium">
						{liveStreams.length} active {liveStreams.length === 1 ? 'stream' : 'streams'}
					</p>
				</div>
			</div>

			<div className="relative group min-w-[160px]">
				<select
					value={selectedOrg}
					onChange={(event) => {
						onOrgChange(event.target.value as StreamOrg);
					}}
					className="w-full appearance-none bg-slate-50 border border-slate-200 text-slate-700 text-sm font-semibold rounded-xl py-2.5 pl-4 pr-10 focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-500 focus-visible:border-sky-500 cursor-pointer hover:bg-slate-100 transition-colors"
					aria-label="Select Stream Org"
				>
					{orgOptions.map((option) => (
						<option key={option.value} value={option.value}>
							{option.label}
						</option>
					))}
				</select>
				<ChevronDown
					className="absolute right-3.5 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none group-hover:text-slate-600 transition-colors"
					size={18}
				/>
			</div>
		</div>

		{liveLoading ? (
			<div className="h-48 flex items-center justify-center text-slate-400 text-sm animate-pulse rounded-xl border border-dashed border-slate-200 bg-slate-50">
				Loading…
			</div>
		) : liveStreams.length === 0 ? (
			<div className="h-48 flex flex-col items-center justify-center text-slate-400 text-sm rounded-xl border border-dashed border-slate-200 bg-slate-50">
				<PlayCircle className="mb-2 opacity-50" size={24} />
				<p>No live streams currently.</p>
			</div>
		) : (
			<div className="max-h-[42rem] overflow-y-auto pr-2 pb-2 custom-scrollbar">
				<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
					{liveStreams.map((stream, streamIndex) => {
						const thumbnail = getThumbnailSource(
							stream.thumbnail ?? undefined,
							"max",
						);
						const linkMeta = getStreamLinkMeta(stream);

						return (
							<a
								key={getStreamKey(stream, streamIndex)}
								href={linkMeta.href}
								target="_blank"
								rel="noreferrer"
								className="group flex flex-col h-full relative rounded-2xl overflow-hidden border border-slate-200 bg-white hover:border-slate-300 hover:shadow-xl hover:-translate-y-1 transition-all duration-300 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-rose-500 focus-visible:ring-offset-2"
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
											className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500 motion-reduce:transition-none"
											onError={onThumbnailError}
										/>
										<div className="absolute top-3 left-3 bg-rose-600 text-white text-[10px] font-black tracking-wider uppercase px-2 py-1 rounded-md flex items-center gap-1.5 shadow-sm">
											<span className="w-1.5 h-1.5 rounded-full bg-white animate-pulse" />
											LIVE
										</div>
									</div>
								) : (
									<div className="aspect-video bg-slate-50 flex flex-col items-center justify-center text-slate-300 border-b border-slate-100">
										<PlayCircle size={32} className="mb-2 opacity-50" />
									</div>
								)}
								<div className="p-5 flex-1 flex flex-col">
									<h4 className="font-bold text-sm leading-snug line-clamp-2 mb-2 text-slate-800 group-hover:text-rose-600 transition-colors">
										{stream.title}
									</h4>
									<div className="mt-auto pt-2 flex items-center justify-between">
										<p className="text-xs font-semibold text-slate-500 truncate pr-2">
											{stream.channel_name}
										</p>
										<span className="inline-flex items-center text-[10px] uppercase tracking-wider font-bold text-rose-600 bg-rose-50 px-2 py-1 rounded-md group-hover:bg-rose-100 transition-colors whitespace-nowrap">
											<ExternalLink size={10} className="mr-1" /> {linkMeta.label}
										</span>
									</div>
								</div>
							</a>
						);
					})}
				</div>
			</div>
		)}
	</div>
);
