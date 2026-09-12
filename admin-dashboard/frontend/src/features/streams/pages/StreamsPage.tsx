import { useQuery } from "@tanstack/react-query";
import { type SyntheticEvent, useState } from "react";
import { queryKeys } from "@/queries/keys";
import { streamsApi } from "@/features/streams/api";
import { LiveStreamsSection } from "@/features/streams/components/LiveStreamsSection";
import { UpcomingStreamsSection } from "@/features/streams/components/UpcomingStreamsSection";
import type { StreamOrg } from "@/features/streams/types";
import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";

export const StreamsPage = () => {
	const [selectedOrg, setSelectedOrg] = useState<StreamOrg>("hololive");

	const orgOptions: Array<{ value: StreamOrg; label: string }> = [
		{ value: "hololive", label: "Hololive" },
		{ value: "vspo", label: "VSpo" },
		{ value: "stellive", label: "Stellive" },
		{ value: "independents", label: "Indie" },
		{ value: "all", label: "All" },
	];

	const liveQuery = useQuery({
		queryKey: queryKeys.streams.live(selectedOrg),
		queryFn: ({ signal }) => streamsApi.getLive(selectedOrg, { signal }),
		refetchInterval: 60 * 1000,
		staleTime: 1000 * 45,
	});

	const upcomingQuery = useQuery({
		queryKey: queryKeys.streams.upcoming(selectedOrg),
		queryFn: ({ signal }) => streamsApi.getUpcoming(selectedOrg, { signal }),
		refetchInterval: 60 * 1000 * 5,
		staleTime: 1000 * 60 * 4,
	});

	const online = useOnline();
	const liveView = queryView(liveQuery, online, data => data.streams.length === 0);
	const upcomingView = queryView(upcomingQuery, online, data => data.streams.length === 0);

	const handleThumbnailError = (event: SyntheticEvent<HTMLImageElement>) => {
		const element = event.currentTarget;
		const fallbackChain =
			element.dataset["fallbackChain"]?.split("|").filter(Boolean) ?? [];
		const nextFallback = fallbackChain.shift();

		if (nextFallback) {
			element.dataset["fallbackChain"] = fallbackChain.join("|");
			element.src = nextFallback;
			return;
		}

		element.style.display = "none";
	};

	return (
		<div className="space-y-6">
			<LiveStreamsSection
				selectedOrg={selectedOrg}
				orgOptions={orgOptions}
				view={liveView}
				onRetry={() => { void liveQuery.refetch(); }}
				onOrgChange={setSelectedOrg}
				onThumbnailError={handleThumbnailError}
			/>
			<UpcomingStreamsSection
				view={upcomingView}
				onRetry={() => { void upcomingQuery.refetch(); }}
				onThumbnailError={handleThumbnailError}
			/>
		</div>
	);
};
