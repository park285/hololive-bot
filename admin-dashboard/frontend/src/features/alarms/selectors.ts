import type { AlarmGroup } from "@/features/alarms/components/AlarmGroups";
import type { Alarm } from "@/features/alarms/types";

export function groupAlarms(alarms: Alarm[]): AlarmGroup[] {
	const groups = new Map<string, AlarmGroup>();
	alarms.forEach((alarm) => {
		const key = `${alarm.roomId}:${alarm.userId}`;
		if (!groups.has(key)) {
			groups.set(key, {
				roomId: alarm.roomId,
				roomName: alarm.roomName,
				userId: alarm.userId,
				userName: alarm.userName,
				alarms: [],
			});
		}
		groups.get(key)?.alarms.push(alarm);
	});

	return Array.from(groups.values()).sort((a, b) => {
		if (a.roomName !== b.roomName) {
			return a.roomName.localeCompare(b.roomName, "ko");
		}
		return a.userName.localeCompare(b.userName, "ko");
	});
}

export function filterAlarmGroups(
	groups: AlarmGroup[],
	keyword: string,
): AlarmGroup[] {
	const normalized = keyword.trim().toLowerCase();
	if (!normalized) {
		return groups;
	}

	return groups.filter(
		(group) =>
			group.roomName.toLowerCase().includes(normalized) ||
			group.userName.toLowerCase().includes(normalized) ||
			group.alarms.some((alarm) =>
				alarm.memberName.toLowerCase().includes(normalized),
			),
	);
}
