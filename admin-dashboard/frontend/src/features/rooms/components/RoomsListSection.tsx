import clsx from "clsx";
import Info from "lucide-react/dist/esm/icons/info.mjs";
import Plus from "lucide-react/dist/esm/icons/plus.mjs";
import Search from "lucide-react/dist/esm/icons/search.mjs";
import Users from "lucide-react/dist/esm/icons/users.mjs";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { VirtualList } from "@/components/ui/VirtualList";
import {
	buildRoomAccessRows,
	filterRoomAccessRows,
} from "@/features/rooms/selectors";
import type { JoinedRoom } from "@/features/rooms/types";

const numberFormatter = new Intl.NumberFormat("ko-KR");

const roomTypeLabel = (type: string): string => {
	if (type.startsWith("O")) return "오픈채팅";
	if (type === "MultiChat") return "그룹채팅";
	if (type === "DirectChat") return "1:1채팅";
	return type || "참여 정보 없음";
};

interface RoomsListSectionProps {
	rooms: string[];
	listTitle: string;
	emptyText: string;
	indicatorClassName: string;
	isBlacklist: boolean;
	infoMessage: string;
	newRoom: string;
	onNewRoomChange: (value: string) => void;
	onAddRoom: () => void;
	onAddRoomId: (chatId: string) => void;
	onRemoveRoom: (room: string) => void;
	addPending: boolean;
	removePending: boolean;
	joinedRooms: JoinedRoom[];
	joinedLoading: boolean;
	joinedUnavailable: boolean;
	actionError: Error | null;
}

export const RoomsListSection = ({
	rooms,
	listTitle,
	emptyText,
	indicatorClassName,
	isBlacklist,
	infoMessage,
	newRoom,
	onNewRoomChange,
	onAddRoom,
	onAddRoomId,
	onRemoveRoom,
	addPending,
	removePending,
	joinedRooms,
	joinedLoading,
	joinedUnavailable,
	actionError,
}: RoomsListSectionProps) => {
	const [search, setSearch] = useState("");
	const rows = useMemo(
		() => buildRoomAccessRows(rooms, joinedRooms),
		[rooms, joinedRooms],
	);
	const filteredRows = useMemo(
		() => filterRoomAccessRows(rows, search),
		[rows, search],
	);
	const actionPending = addPending || removePending;
	const registeredLabel = isBlacklist ? "차단됨" : "허용됨";
	const availableLabel = isBlacklist ? "응답 허용" : "허용 안 됨";
	const addAction = isBlacklist ? "차단" : "허용";
	const removeAction = isBlacklist ? "차단 해제" : "허용 해제";

	return (
		<div className="space-y-4">
			<div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
				<div className="flex items-center gap-3">
					<h3 className="text-lg font-display font-bold text-foreground text-balance">
						{listTitle}
					</h3>
					<Badge
						variant="secondary"
						className="text-muted-foreground tabular-nums"
					>
						{numberFormatter.format(rooms.length)}개 {isBlacklist ? "차단" : "허용"}
					</Badge>
				</div>

				<div className="relative w-full sm:w-80">
					<Label htmlFor="room-access-search" className="sr-only">
						채팅방 검색
					</Label>
					<Search
						size={16}
						className="absolute left-3 top-1/2 -translate-y-1/2 text-subtle-foreground"
						aria-hidden="true"
					/>
					<Input
						id="room-access-search"
						name="room-access-search"
						type="search"
						autoComplete="off"
						spellCheck={false}
						value={search}
						onChange={(event) => {
							setSearch(event.target.value);
						}}
						placeholder="방 이름 또는 ID로 검색…"
						className={clsx(
							"pl-9 focus-visible:ring-2",
							isBlacklist
								? "focus-visible:ring-rose-200"
								: "focus-visible:ring-blue-200",
						)}
					/>
				</div>
			</div>

			<div className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
				<div
					className={clsx(
						"h-1",
						isBlacklist
							? "bg-linear-to-r from-rose-400 to-rose-500"
							: "bg-linear-to-r from-sky-400 to-cyan-400",
					)}
				/>

				<div className="flex items-start gap-2 border-b border-border-subtle bg-muted/40 px-5 py-3 text-sm text-muted-foreground">
					<Info size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
					<p>{infoMessage}</p>
				</div>

				{joinedUnavailable && (
					<div
						role="status"
						className="border-b border-amber-200 bg-amber-50 px-5 py-3 text-sm text-amber-800"
					>
						참여 중인 방 목록을 불러오지 못했습니다. 현재 등록된 방은 계속 관리할 수 있습니다.
					</div>
				)}

				{actionError && (
					<div
						role="alert"
						className="border-b border-rose-200 bg-rose-50 px-5 py-3 text-sm text-rose-700"
					>
						변경사항을 저장하지 못했습니다. 잠시 후 다시 시도해 주세요.
					</div>
				)}

				{joinedLoading ? (
					<div
						className="py-12 text-center text-sm text-subtle-foreground"
						aria-busy="true"
					>
						참여 중인 방 목록을 불러오는 중…
					</div>
				) : filteredRows.length === 0 ? (
					<div className="py-12 text-center text-sm text-subtle-foreground">
						{search.trim() ? "검색 결과가 없습니다." : emptyText}
					</div>
				) : (
					<VirtualList
						items={filteredRows}
						estimateSize={() => 76}
						getItemKey={(row) => row.key}
						recomputeKey={`${search}:${String(actionPending)}`}
						className="max-h-[34rem]"
						itemClassName="border-b border-border-subtle"
						renderItem={(row) => {
							const displayName = row.name || row.chatId;
							return (
								<div className="flex items-center gap-4 bg-card px-5 py-4 transition-colors hover:bg-accent/50 focus-within:bg-accent/50">
									<div
										className={clsx(
											"h-2 w-2 shrink-0 rounded-full",
											row.registered
												? indicatorClassName
												: "bg-slate-300 dark:bg-slate-600",
										)}
										aria-hidden="true"
									/>
									<div className="min-w-0 flex-1">
										<div className="flex flex-wrap items-center gap-2">
											<span className="truncate font-medium text-foreground">
												{displayName}
											</span>
											<Badge
												variant="secondary"
												className={clsx(
													"shrink-0 text-[10px]",
													row.registered
														? isBlacklist
															? "bg-rose-50 text-rose-700"
															: "bg-sky-50 text-sky-700"
														: "text-muted-foreground",
												)}
											>
												{row.registered ? registeredLabel : availableLabel}
											</Badge>
											{!row.joined && (
												<Badge
													variant="outline"
													className="shrink-0 text-[10px] text-muted-foreground"
												>
													참여 정보 없음
												</Badge>
											)}
										</div>
										<div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-subtle-foreground">
											{row.name && (
												<span
													className="select-all font-mono"
													translate="no"
												>
													{row.chatId}
												</span>
											)}
											{row.joined && (
												<>
													<span>{roomTypeLabel(row.type)}</span>
													<span className="inline-flex items-center gap-1 tabular-nums">
														<Users size={12} aria-hidden="true" />
														{numberFormatter.format(row.memberCount)}명
													</span>
												</>
											)}
										</div>
									</div>
									<Button
										type="button"
										variant={
											row.registered
												? "outline"
												: isBlacklist
													? "destructive"
													: "default"
										}
										size="sm"
										disabled={actionPending}
										onClick={() => {
											if (row.registered) {
												onRemoveRoom(row.aclValue);
											} else {
												onAddRoomId(row.chatId);
											}
										}}
										className={clsx(
											"shrink-0",
											row.registered &&
												(isBlacklist
													? "border-rose-200 text-rose-700 hover:bg-rose-50 hover:text-rose-800"
													: "border-sky-200 text-sky-700 hover:bg-sky-50 hover:text-sky-800"),
										)}
									>
										{row.registered ? removeAction : addAction}
									</Button>
								</div>
							);
						}}
					/>
				)}
			</div>

			<div className="rounded-xl border border-dashed border-border bg-muted/30 p-4">
				<div className="flex flex-col gap-3 md:flex-row md:items-end">
					<div className="min-w-0 flex-1 space-y-1.5">
						<Label
							htmlFor="new-room-id"
							className="flex items-center gap-2 text-sm font-semibold text-foreground"
						>
							<Plus
								size={16}
								className={isBlacklist ? "text-rose-500" : "text-sky-500"}
								aria-hidden="true"
							/>
							목록에 없는 채팅방 ID 직접 등록
						</Label>
						<Input
							id="new-room-id"
							name="room-id"
							autoComplete="off"
							inputMode="numeric"
							spellCheck={false}
							value={newRoom}
							onChange={(event) => {
								onNewRoomChange(event.target.value);
							}}
							onKeyDown={(event) => {
								if (event.key === "Enter") onAddRoom();
							}}
							placeholder="예: 200000000000002…"
							className="font-mono"
							disabled={actionPending}
						/>
					</div>
					<Button
						type="button"
						onClick={onAddRoom}
						disabled={actionPending || !newRoom.trim()}
						variant={isBlacklist ? "destructive" : "default"}
						className="md:min-w-28"
					>
						{addPending ? "등록 중…" : `${addAction} 등록`}
					</Button>
				</div>
			</div>
		</div>
	);
};
