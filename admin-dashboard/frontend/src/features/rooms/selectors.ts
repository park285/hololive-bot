import type { JoinedRoom } from "@/features/rooms/types";

export interface RoomAccessRow {
	key: string;
	aclValue: string;
	chatId: string;
	name: string;
	type: string;
	memberCount: number;
	registered: boolean;
	joined: boolean;
}

export function buildRoomAccessRows(
	rooms: string[],
	joinedRooms: JoinedRoom[],
): RoomAccessRow[] {
	const unmatchedRooms = new Set(rooms);
	const rows = joinedRooms.map((joinedRoom) => {
		const name = joinedRoom.name.trim();
		const matchedRoom = unmatchedRooms.has(joinedRoom.chatId)
			? joinedRoom.chatId
			: name !== "" && unmatchedRooms.has(name)
				? name
				: null;

		if (matchedRoom !== null) {
			unmatchedRooms.delete(matchedRoom);
		}

		return {
			key: `joined:${joinedRoom.chatId}`,
			aclValue: matchedRoom ?? joinedRoom.chatId,
			chatId: joinedRoom.chatId,
			name,
			type: joinedRoom.type,
			memberCount: joinedRoom.memberCount,
			registered: matchedRoom !== null,
			joined: true,
		};
	});

	for (const room of unmatchedRooms) {
		rows.push({
			key: `registered:${room}`,
			aclValue: room,
			chatId: room,
			name: "",
			type: "",
			memberCount: 0,
			registered: true,
			joined: false,
		});
	}

	return rows.sort((left, right) => {
		if (left.registered !== right.registered) {
			return left.registered ? -1 : 1;
		}

		const leftLabel = left.name || left.chatId;
		const rightLabel = right.name || right.chatId;
		return leftLabel.localeCompare(rightLabel, "ko-KR");
	});
}

export function filterRoomAccessRows(
	rows: RoomAccessRow[],
	search: string,
): RoomAccessRow[] {
	const keyword = search.trim().toLocaleLowerCase("ko-KR");
	if (keyword === "") return rows;

	return rows.filter(
		(row) =>
			row.name.toLocaleLowerCase("ko-KR").includes(keyword) ||
			row.chatId.toLocaleLowerCase("ko-KR").includes(keyword) ||
			row.aclValue.toLocaleLowerCase("ko-KR").includes(keyword),
	);
}
