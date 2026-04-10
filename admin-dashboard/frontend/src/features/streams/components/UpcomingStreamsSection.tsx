import Calendar from "lucide-react/dist/esm/icons/calendar";
import ExternalLink from "lucide-react/dist/esm/icons/external-link";
import PlayCircle from "lucide-react/dist/esm/icons/play-circle";
import type { SyntheticEvent } from "react";
import {
	getStreamKey,
	getStreamLinkMeta,
	getThumbnailSource,
} from "@/features/streams/lib/media";
import type { Stream } from "@/features/streams/types";

interface UpcomingStreamsSectionProps {
	upcomingStreams: Stream[];
	upcomingLoading: boolean;
	onThumbnailError: (event: SyntheticEvent<HTMLImageElement>) => void;
}

export const UpcomingStreamsSection = ({
	upcomingStreams,
	upcomingLoading,
	onThumbnailError,
}: UpcomingStreamsSectionProps) => (
	<div className="bg-white rounded-xl shadow-sm border border-slate-200 p-6">
		<div className="flex items-center gap-2 mb-4">
			<Calendar className="text-sky-500" />
			<h3 className="text-lg font-bold text-slate-800">
				Upcoming Streams (24h)
			</h3>
			<span className="text-xs font-medium px-2 py-0.5 rounded-full bg-sky-100 text-sky-600">
				{upcomingStreams.length}
			</span>
		</div>

		{upcomingLoading ? (
			<div className="h-40 flex items-center justify-center text-slate-400 text-sm">
				Loading…
			</div>
		) : (
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
				{upcomingStreams.map((stream, index) => {
					const thumbnail = getThumbnailSource(
						stream.thumbnail ?? undefined,
						"sd",
					);
					const linkMeta = getStreamLinkMeta(stream);

					return (
						<a
							key={getStreamKey(stream, index)}
							href={linkMeta.href}
							target="_blank"
							rel="noreferrer"
							className="flex items-center p-3 rounded-lg border border-slate-100 hover:bg-slate-50 transition-colors group [content-visibility:auto] contain-intrinsic-size-[80px]"
						>
							<div className="w-20 h-14 rounded-lg overflow-hidden shrink-0 bg-slate-100 mr-4 relative flex items-center justify-center text-slate-300">
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
								<h4 className="font-medium text-sm text-slate-900 truncate group-hover:text-sky-600 transition-colors">
									{stream.title}
								</h4>
								<p className="text-xs text-slate-500 mt-0.5">
									{stream.channel_name}
								</p>
							</div>
							<div className="ml-4 text-right shrink-0 flex flex-col items-end gap-1">
								<div className="text-xs font-bold text-slate-700 bg-slate-100 px-2 py-1 rounded whitespace-nowrap">
									{stream.start_scheduled
										? new Date(stream.start_scheduled).toLocaleTimeString([], {
												hour: "2-digit",
												minute: "2-digit",
											})
										: "TBA"}
								</div>
								<span className="inline-flex items-center gap-1 text-[10px] text-red-600 hover:text-red-700 hover:bg-red-50 px-2 py-0.5 rounded transition-colors">
									{linkMeta.badge}
									<ExternalLink size={10} />
								</span>
							</div>
						</a>
					);
				})}
				{upcomingStreams.length === 0 && (
					<p className="col-span-full text-center text-slate-400 text-sm py-10">
						No upcoming streams found.
					</p>
				)}
			</div>
		)}
	</div>
);
