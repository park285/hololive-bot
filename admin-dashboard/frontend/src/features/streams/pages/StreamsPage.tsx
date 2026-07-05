import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { type SyntheticEvent, useState } from "react";
import { queryKeys } from "@/api/queryKeys";
import { streamsApi } from "@/features/streams/api";
import { LiveStreamsSection } from "@/features/streams/components/LiveStreamsSection";
import { UpcomingStreamsSection } from "@/features/streams/components/UpcomingStreamsSection";
import type { StreamOrg } from "@/features/streams/types";
import { visibleRefetchInterval } from "@/lib/polling";
import { type SectionStateProps, sectionStateProps } from "@/lib/queryState";

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
		queryFn: () => streamsApi.getLive(selectedOrg),
		refetchInterval: visibleRefetchInterval(60 * 1000),
		staleTime: 1000 * 45,
		placeholderData: keepPreviousData,
	});

	const upcomingQuery = useQuery({
		queryKey: queryKeys.streams.upcoming(selectedOrg),
		queryFn: () => streamsApi.getUpcoming(selectedOrg),
		refetchInterval: visibleRefetchInterval(60 * 1000 * 5),
		staleTime: 1000 * 60 * 4,
		placeholderData: keepPreviousData,
	});

	const liveStreams = liveQuery.data?.streams ?? [];
	const upcomingStreams = upcomingQuery.data?.streams ?? [];

	const liveState: SectionStateProps = {
		...sectionStateProps(liveQuery),
		isError: liveQuery.isError && liveStreams.length === 0,
	};
	const upcomingState: SectionStateProps = {
		...sectionStateProps(upcomingQuery),
		isError: upcomingQuery.isError && upcomingStreams.length === 0,
	};

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
				state={liveState}
				onOrgChange={setSelectedOrg}
				onThumbnailError={handleThumbnailError}
			/>
			<UpcomingStreamsSection
				upcomingStreams={upcomingStreams}
				state={upcomingState}
				onThumbnailError={handleThumbnailError}
			/>
		</div>
	);
};
