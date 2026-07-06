import clsx from "clsx";
import Check from "lucide-react/dist/esm/icons/check.mjs";
import Info from "lucide-react/dist/esm/icons/info.mjs";
import Search from "lucide-react/dist/esm/icons/search.mjs";
import Users from "lucide-react/dist/esm/icons/users.mjs";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { VirtualList } from "@/components/ui/VirtualList";
import type { JoinedRoom } from "@/features/rooms/types";

const numberFormatter = new Intl.NumberFormat("ko-KR");

const roomTypeLabel = (type: string): string => {
	if (type.startsWith("O")) return "오픈채팅";
	if (type === "MultiChat") return "그룹채팅";
	if (type === "DirectChat") return "1:1채팅";
	return type || "채팅방";
};

const roomDisplayName = (room: JoinedRoom): string =>
	room.name.trim() !== "" ? room.name : room.chatId;

interface RoomPickerRow {
	room: JoinedRoom;
	added: boolean;
}

interface RoomPickerProps {
	joinedRooms: JoinedRoom[];
	existingRooms: string[];
	isBlacklist: boolean;
	loading: boolean;
	unavailable: boolean;
	onAddRoom: (chatId: string) => void;
	addPending: boolean;
	newRoom: string;
	onNewRoomChange: (value: string) => void;
	onAddManualRoom: () => void;
}

export const RoomPicker = ({
	joinedRooms,
	existingRooms,
	isBlacklist,
	loading,
	unavailable,
	onAddRoom,
	addPending,
	newRoom,
	onNewRoomChange,
	onAddManualRoom,
}: RoomPickerProps) => {
	const [search, setSearch] = useState("");
	const [selectedChatId, setSelectedChatId] = useState<string | null>(null);
	const [manualMode, setManualMode] = useState(false);

	const existingSet = useMemo(
		() => new Set(existingRooms),
		[existingRooms],
	);

	const rows = useMemo<RoomPickerRow[]>(() => {
		const keyword = search.trim().toLowerCase();
		return joinedRooms
			.filter((room) => {
				if (keyword === "") return true;
				return (
					room.name.toLowerCase().includes(keyword) ||
					room.chatId.includes(keyword)
				);
			})
			.map((room) => ({
				room,
				added:
					existingSet.has(room.chatId) ||
					(room.name.trim() !== "" && existingSet.has(room.name)),
			}));
	}, [joinedRooms, existingSet, search]);

	const selectedRoom = useMemo(
		() => joinedRooms.find((room) => room.chatId === selectedChatId) ?? null,
		[joinedRooms, selectedChatId],
	);

	const themeRing = isBlacklist
		? "focus-visible:ring-rose-200"
		: "focus-visible:ring-blue-200";
	const themeButton = isBlacklist
		? "bg-linear-to-r from-rose-500 to-rose-600 hover:from-rose-600 hover:to-rose-700 shadow-rose-200"
		: "bg-linear-to-r from-sky-500 to-cyan-500 hover:from-sky-600 hover:to-cyan-600 shadow-sky-200";

	const handleAddSelected = () => {
		if (!selectedRoom) return;
		onAddRoom(selectedRoom.chatId);
		setSelectedChatId(null);
		setSearch("");
	};

	const manualInput = (
		<div className="space-y-1.5">
			<Label
				htmlFor="new-room-id"
				className="text-xs font-semibold text-muted-foreground"
			>
				채팅방 ID (RoomID)
			</Label>
			<Input
				id="new-room-id"
				value={newRoom}
				onChange={(event) => {
					onNewRoomChange(event.target.value);
				}}
				onKeyDown={(event) => {
					if (event.key === "Enter") {
						onAddManualRoom();
					}
				}}
				placeholder="예: 200000000000002"
				className={clsx("font-mono focus-visible:ring-2", themeRing)}
				disabled={addPending}
			/>
			<Button
				onClick={onAddManualRoom}
				disabled={addPending || !newRoom.trim()}
				className={clsx("w-full shadow-sm", themeButton)}
				aria-label="채팅방 ID 직접 추가하기"
			>
				{addPending ? "추가 중…" : "직접 추가"}
			</Button>
		</div>
	);

	const showManualOnly = unavailable || (!loading && joinedRooms.length === 0);

	if (showManualOnly) {
		return (
			<div className="space-y-3">
				<div className="p-3 rounded-lg flex items-start gap-2 border border-border bg-muted">
					<Info
						className="shrink-0 mt-0.5 text-muted-foreground"
						size={16}
						aria-hidden="true"
					/>
					<p className="text-xs leading-snug text-muted-foreground">
						{unavailable
							? "참여 중인 방 목록을 불러올 수 없습니다. 채팅방 ID를 직접 입력해 추가하세요."
							: "봇이 참여 중인 방이 없습니다. 채팅방 ID를 직접 입력해 추가하세요."}
					</p>
				</div>
				{manualInput}
			</div>
		);
	}

	return (
		<div className="space-y-3">
			<div className="space-y-1.5">
				<Label
					htmlFor="room-picker-search"
					className="text-xs font-semibold text-muted-foreground"
				>
					봇이 참여 중인 방에서 선택
				</Label>
				<div className="relative">
					<Search
						size={15}
						className="absolute left-2.5 top-1/2 -translate-y-1/2 text-subtle-foreground"
						aria-hidden="true"
					/>
					<Input
						id="room-picker-search"
						value={search}
						onChange={(event) => {
							setSearch(event.target.value);
						}}
						placeholder="방 이름 또는 ID로 검색"
						className={clsx("pl-8 focus-visible:ring-2", themeRing)}
						aria-label="방 검색"
					/>
				</div>
			</div>

			<div className="rounded-lg border border-border overflow-hidden bg-card">
				{loading ? (
					<div className="text-subtle-foreground text-center text-sm py-8">
						방 목록을 불러오는 중…
					</div>
				) : rows.length === 0 ? (
					<div className="text-subtle-foreground text-center text-sm py-8">
						검색 결과가 없습니다.
					</div>
				) : (
					<VirtualList
						items={rows}
						estimateSize={() => 60}
						getItemKey={(row) => row.room.chatId}
						recomputeKey={selectedChatId}
						role="listbox"
						className="max-h-72"
						renderItem={({ room, added }) => {
							const selected = room.chatId === selectedChatId;
							return (
								<button
									type="button"
									role="option"
									aria-selected={selected}
									disabled={added}
									onClick={() => {
										setSelectedChatId(selected ? null : room.chatId);
									}}
									className={clsx(
										"w-full text-left px-3 py-2.5 flex items-center gap-3 border-b border-border-subtle transition-colors",
										added
											? "opacity-60 cursor-not-allowed bg-muted/40"
											: selected
												? isBlacklist
													? "bg-rose-50"
													: "bg-sky-50"
												: "hover:bg-accent",
									)}
								>
									<div className="min-w-0 flex-1">
										<div className="flex items-center gap-1.5">
											<span className="font-semibold text-sm text-foreground truncate">
												{roomDisplayName(room)}
											</span>
											{added && (
												<Badge
													variant="secondary"
													className="shrink-0 text-[10px] px-1.5 py-0 text-muted-foreground"
												>
													추가됨
												</Badge>
											)}
										</div>
										<div className="flex items-center gap-2 mt-0.5">
											<span className="font-mono text-[11px] text-subtle-foreground truncate">
												{room.chatId}
											</span>
										</div>
									</div>
									<div className="flex items-center gap-2 shrink-0">
										<span className="text-[11px] text-muted-foreground">
											{roomTypeLabel(room.type)}
										</span>
										<span className="inline-flex items-center gap-0.5 text-[11px] text-subtle-foreground tabular-nums">
											<Users size={12} aria-hidden="true" />
											{numberFormatter.format(room.memberCount)}
										</span>
										{selected && !added && (
											<Check
												size={16}
												className={isBlacklist ? "text-rose-500" : "text-sky-500"}
												aria-hidden="true"
											/>
										)}
									</div>
								</button>
							);
						}}
					/>
				)}
			</div>

			{selectedRoom && (
				<div
					className={clsx(
						"p-2.5 rounded-lg border text-xs flex items-center gap-2",
						isBlacklist
							? "bg-rose-50 border-rose-100 text-rose-700"
							: "bg-sky-50 border-sky-100 text-sky-700",
					)}
				>
					<Check size={14} className="shrink-0" aria-hidden="true" />
					<span className="truncate">
						<span className="font-semibold">{roomDisplayName(selectedRoom)}</span>
						<span className="font-mono ml-1.5 opacity-70">
							{selectedRoom.chatId}
						</span>
					</span>
				</div>
			)}

			<Button
				onClick={handleAddSelected}
				disabled={addPending || !selectedRoom}
				className={clsx("w-full shadow-sm", themeButton)}
				aria-label="선택한 채팅방 추가하기"
			>
				{addPending ? "추가 중…" : "추가하기"}
			</Button>

			<div className="pt-1 border-t border-border-subtle">
				<button
					type="button"
					onClick={() => {
						setManualMode((prev) => !prev);
					}}
					className="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2 transition-colors"
					aria-expanded={manualMode}
				>
					{manualMode ? "직접 입력 닫기" : "찾는 방이 없나요? ID 직접 입력"}
				</button>
			</div>

			{manualMode && <div className="pt-1">{manualInput}</div>}
		</div>
	);
};
