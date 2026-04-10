import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { type SyntheticEvent, useState } from "react";
import { queryKeys } from "@/api/queryKeys";
import { streamsApi } from "@/features/streams/api";
import { LiveStreamsSection } from "@/features/streams/components/LiveStreamsSection";
import { UpcomingStreamsSection } from "@/features/streams/components/UpcomingStreamsSection";
import type { StreamOrg } from "@/features/streams/types";

export const StreamsPage = () => {
	const [selectedOrg, setSelectedOrg] = useState<StreamOrg>("hololive");

	const orgOptions: Array<{ value: StreamOrg; label: string }> = [
		{ value: "hololive", label: "Hololive" },
		{ value: "vspo", label: "VSpo" },
		{ value: "stellive", label: "Stellive" },
		{ value: "independents", label: "Indie" },
		{ value: "all", label: "All" },
	];

	const { data: liveData, isLoading: liveLoading } = useQuery({
		queryKey: queryKeys.streams.live(selectedOrg),
		queryFn: () => streamsApi.getLive(selectedOrg),
		refetchInterval: 60 * 1000,
		staleTime: 1000 * 45,
		placeholderData: keepPreviousData,
	});

	const { data: upcomingData, isLoading: upcomingLoading } = useQuery({
		queryKey: queryKeys.streams.upcoming(selectedOrg),
		queryFn: () => streamsApi.getUpcoming(selectedOrg),
		refetchInterval: 60 * 1000 * 5,
		staleTime: 1000 * 60 * 4,
		placeholderData: keepPreviousData,
	});

	const liveStreams = liveData?.streams ?? [];
	const upcomingStreams = upcomingData?.streams ?? [];

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
				liveStreams={liveStreams}
				liveLoading={liveLoading}
				onOrgChange={setSelectedOrg}
				onThumbnailError={handleThumbnailError}
			/>
			<UpcomingStreamsSection
				upcomingStreams={upcomingStreams}
				upcomingLoading={upcomingLoading}
				onThumbnailError={handleThumbnailError}
			/>
		</div>
	);
};
