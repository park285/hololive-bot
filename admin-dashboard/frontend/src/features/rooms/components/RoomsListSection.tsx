import clsx from "clsx";
import Info from "lucide-react/dist/esm/icons/info";
import Plus from "lucide-react/dist/esm/icons/plus";
import Trash2 from "lucide-react/dist/esm/icons/trash-2";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { VirtualList } from "@/components/ui/VirtualList";

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
	onDeleteRoom: (room: string) => void;
	addPending: boolean;
	removePending: boolean;
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
	onDeleteRoom,
	addPending,
	removePending,
}: RoomsListSectionProps) => (
	<div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
		<div className="lg:col-span-2 space-y-6">
			<div className="flex items-center justify-between">
				<h3 className="text-lg font-display font-bold text-slate-900">
					{listTitle}
				</h3>
				<Badge variant="secondary" className="text-slate-600 tabular-nums">
					{numberFormatter.format(rooms.length)}개
				</Badge>
			</div>

			<div
				className="bg-white rounded-xl border border-slate-200 shadow-sm divide-y divide-slate-100 overflow-hidden"
			>
				{rooms.length === 0 ? (
					<div className="text-slate-400 text-center py-12 flex flex-col items-center gap-2">
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
						itemClassName="border-b border-slate-100"
						renderItem={(room) => (
							<div
								key={room}
								role="listitem"
								className="flex items-center justify-between px-6 py-4 hover:bg-slate-50 transition-colors group focus-within:bg-slate-50 bg-white"
							>
								<div className="flex items-center gap-3">
									<div
										className={clsx("w-2 h-2 rounded-full", indicatorClassName)}
										aria-hidden="true"
									/>
									<span className="font-mono text-slate-700 font-medium select-all">
										{room}
									</span>
								</div>
								<Button
									variant="ghost"
									size="sm"
									onClick={() => {
										onDeleteRoom(room);
									}}
									disabled={removePending}
									className="text-slate-400 hover:text-red-600 hover:bg-red-50 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 transition-all focus-visible:ring-2 focus-visible:ring-red-200"
									aria-label={`${room} 방 삭제`}
								>
									<Trash2 size={16} aria-hidden="true" />
								</Button>
							</div>
						)}
					/>
				)}
			</div>
		</div>

		<div>
			<Card className="sticky top-6">
				<div className="p-5 space-y-4">
					<h3 className="font-display font-bold text-slate-900 flex items-center gap-2">
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

					<div className="space-y-3">
						<div className="space-y-1.5">
							<Label
								htmlFor="new-room-id"
								className="text-xs font-semibold text-slate-500"
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
										onAddRoom();
									}
								}}
								placeholder="예: 451788135895779"
								className={clsx(
									"font-mono focus-visible:ring-2",
									isBlacklist
										? "focus-visible:ring-rose-200"
										: "focus-visible:ring-blue-200",
								)}
								disabled={addPending}
							/>
						</div>
						<Button
							onClick={onAddRoom}
							disabled={addPending || !newRoom.trim()}
							className={clsx(
								"w-full shadow-sm",
								isBlacklist
									? "bg-rose-600 hover:bg-rose-700 shadow-rose-200"
									: "bg-blue-600 hover:bg-blue-700 shadow-blue-200",
							)}
							aria-label="채팅방 추가하기"
						>
							{addPending ? "추가 중…" : "추가하기"}
						</Button>
					</div>
				</div>
			</Card>
		</div>
	</div>
);
