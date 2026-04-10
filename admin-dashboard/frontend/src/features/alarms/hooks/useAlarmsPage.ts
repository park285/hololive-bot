import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { queryKeys } from "@/api/queryKeys";
import { alarmsApi, namesApi } from "@/features/alarms/api";
import { filterAlarmGroups, groupAlarms } from "@/features/alarms/selectors";
import type { Alarm } from "@/features/alarms/types";

const ALARM_GROUP_PAGE_SIZE = 20;

export function useAlarmsPage() {
	const queryClient = useQueryClient();
	const [search, setSearch] = useState("");
	const deferredSearch = useDeferredValue(search);
	const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
	const [alarmToDelete, setAlarmToDelete] = useState<Alarm | null>(null);
	const [visibleGroupCount, setVisibleGroupCount] = useState(
		ALARM_GROUP_PAGE_SIZE,
	);
	const [editModal, setEditModal] = useState<{
		type: "room" | "user";
		id: string;
		currentName: string;
	} | null>(null);

	const query = useQuery({
		queryKey: queryKeys.alarms.all,
		queryFn: alarmsApi.getAll,
	});

	const deleteAlarmMutation = useMutation({
		mutationFn: alarmsApi.delete,
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: queryKeys.alarms.all });
		},
	});

	const setNameMutation = useMutation({
		mutationFn: async ({
			type,
			id,
			name,
		}: {
			type: "room" | "user";
			id: string;
			name: string;
		}) =>
			type === "room"
				? namesApi.setRoomName(id, name)
				: namesApi.setUserName(id, name),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: queryKeys.alarms.all });
		},
	});

	useEffect(() => {
		setVisibleGroupCount(ALARM_GROUP_PAGE_SIZE);
	}, [deferredSearch]);

	const groupedAlarms = useMemo(
		() => groupAlarms(query.data?.alarms ?? []),
		[query.data?.alarms],
	);
	const filteredGroups = useMemo(
		() => filterAlarmGroups(groupedAlarms, deferredSearch),
		[groupedAlarms, deferredSearch],
	);
	const totalAlarms = filteredGroups.reduce(
		(sum, group) => sum + group.alarms.length,
		0,
	);

	return {
		search,
		setSearch,
		expandedGroups,
		setExpandedGroups,
		alarmToDelete,
		setAlarmToDelete,
		visibleGroupCount,
		setVisibleGroupCount,
		editModal,
		setEditModal,
		groupedAlarms,
		filteredGroups,
		totalAlarms,
		query,
		deleteAlarmMutation,
		setNameMutation,
	};
}
