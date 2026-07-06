import clsx from "clsx";
import Info from "lucide-react/dist/esm/icons/info.mjs";
import Plus from "lucide-react/dist/esm/icons/plus.mjs";
import Trash2 from "lucide-react/dist/esm/icons/trash-2.mjs";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { VirtualList } from "@/components/ui/VirtualList";
import { RoomPicker } from "@/features/rooms/components/RoomPicker";
import type { JoinedRoom } from "@/features/rooms/types";

const numberFormatter = new Intl.NumberFormat("ko-KR");

interface RoomsListSectionProps {
	rooms: string[];
	listTitle: string;
	emptyText: string;
	addTitle: string;
	indicatorClassName: string;
	isBlacklist: boolean;
	infoMessage: string;
	newRoom: string;
	onNewRoomChange: (value: string) => void;
	onAddRoom: () => void;
	onAddRoomId: (chatId: string) => void;
	onDeleteRoom: (room: string) => void;
	addPending: boolean;
	removePending: boolean;
	joinedRooms: JoinedRoom[];
	joinedRoomsMap: Map<string, JoinedRoom>;
	joinedLoading: boolean;
	joinedUnavailable: boolean;
}

export const RoomsListSection = ({
	rooms,
	listTitle,
	emptyText,
	addTitle,
	indicatorClassName,
	isBlacklist,
	infoMessage,
	newRoom,
	onNewRoomChange,
	onAddRoom,
	onAddRoomId,
	onDeleteRoom,
	addPending,
	removePending,
	joinedRooms,
	joinedRoomsMap,
	joinedLoading,
	joinedUnavailable,
}: RoomsListSectionProps) => (
	<div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
		<div className="lg:col-span-2 space-y-6">
			<div className="flex items-center justify-between">
				<h3 className="text-lg font-display font-bold text-foreground">
					{listTitle}
				</h3>
				<Badge variant="secondary" className="text-muted-foreground tabular-nums">
					{numberFormatter.format(rooms.length)}개
				</Badge>
			</div>

			<div
				className="relative bg-card rounded-2xl border border-border shadow-sm divide-y divide-border-subtle overflow-hidden"
			>
				<div className={clsx("absolute top-0 left-0 right-0 h-1", isBlacklist ? "bg-linear-to-r from-rose-400 to-rose-500" : "bg-linear-to-r from-sky-400 to-cyan-400")} />
				{rooms.length === 0 ? (
					<div className="text-subtle-foreground text-center py-12 flex flex-col items-center gap-2">
						<Info size={32} className="opacity-20" aria-hidden="true" />
						<p>{emptyText}</p>
					</div>
				) : (
					<VirtualList
						items={rooms}
						estimateSize={() => 68}
						getItemKey={(room) => room}
						recomputeKey={removePending}
						className="max-h-[32rem]"
						itemClassName="border-b border-border-subtle"
						renderItem={(room) => {
							const joined = joinedRoomsMap.get(room);
							const displayName =
								joined && joined.name.trim() !== "" ? joined.name : null;
							return (
								<div
									key={room}
									role="listitem"
									className="flex items-center justify-between px-6 py-4 hover:bg-linear-to-r hover:from-sky-50/40 hover:to-transparent transition-colors group focus-within:bg-sky-50/40 bg-card"
								>
									<div className="flex items-center gap-3 min-w-0">
										<div
											className={clsx("w-2 h-2 rounded-full shrink-0", indicatorClassName)}
											aria-hidden="true"
										/>
										<div className="min-w-0">
											{displayName ? (
												<>
													<div className="text-foreground font-medium truncate">
														{displayName}
													</div>
													<div className="font-mono text-xs text-subtle-foreground truncate select-all">
														{room}
													</div>
												</>
											) : (
												<span className="font-mono text-foreground font-medium select-all">
													{room}
												</span>
											)}
										</div>
									</div>
									<Button
										variant="ghost"
										size="sm"
										onClick={() => {
											onDeleteRoom(room);
										}}
										disabled={removePending}
										className="text-subtle-foreground hover:text-red-600 hover:bg-red-50 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all focus-visible:ring-2 focus-visible:ring-red-200 shrink-0 ml-2"
										aria-label={`${displayName ?? room} 방 삭제`}
									>
										<Trash2 size={16} aria-hidden="true" />
									</Button>
								</div>
							);
						}}
					/>
				)}
			</div>
		</div>

		<div>
			<Card className="sticky top-6">
				<div className="p-5 space-y-4">
					<h3 className="font-display font-bold text-foreground flex items-center gap-2">
						<Plus
							className={isBlacklist ? "text-rose-500" : "text-blue-500"}
							size={18}
							aria-hidden="true"
						/>{" "}
						{addTitle}
					</h3>

					<div
						className={clsx(
							"p-3 rounded-lg flex items-start gap-2 border",
							isBlacklist
								? "bg-rose-50 border-rose-100"
								: "bg-blue-50 border-blue-100",
						)}
					>
						<Info
							className={clsx(
								"shrink-0 mt-0.5",
								isBlacklist ? "text-rose-600" : "text-blue-600",
							)}
							size={16}
							aria-hidden="true"
						/>
						<p
							className={clsx(
								"text-xs leading-snug",
								isBlacklist ? "text-rose-700" : "text-blue-700",
							)}
						>
							{infoMessage}
						</p>
					</div>

					<RoomPicker
						joinedRooms={joinedRooms}
						existingRooms={rooms}
						isBlacklist={isBlacklist}
						loading={joinedLoading}
						unavailable={joinedUnavailable}
						onAddRoom={onAddRoomId}
						addPending={addPending}
						newRoom={newRoom}
						onNewRoomChange={onNewRoomChange}
						onAddManualRoom={onAddRoom}
					/>
				</div>
			</Card>
		</div>
	</div>
);
