import Bell from "lucide-react/dist/esm/icons/bell";
import ChevronDown from "lucide-react/dist/esm/icons/chevron-down";
import ChevronUp from "lucide-react/dist/esm/icons/chevron-up";
import Edit2 from "lucide-react/dist/esm/icons/edit-2";
import MapPin from "lucide-react/dist/esm/icons/map-pin";
import Trash2 from "lucide-react/dist/esm/icons/trash-2";
import User from "lucide-react/dist/esm/icons/user";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { VirtualList } from "@/components/ui/VirtualList";
import type { Alarm } from "@/features/alarms/types";

const numberFormatter = new Intl.NumberFormat("ko-KR");
const GROUP_PREVIEW_COUNT = 5;
const GROUP_GRID_COLUMNS = 3;

const chunkAlarmGroups = (groups: AlarmGroup[]) => {
	const rows: AlarmGroup[][] = [];
	for (let index = 0; index < groups.length; index += GROUP_GRID_COLUMNS) {
		rows.push(groups.slice(index, index + GROUP_GRID_COLUMNS));
	}
	return rows;
};

export interface AlarmGroup {
	roomId: string;
	roomName: string;
	userId: string;
	userName: string;
	alarms: Alarm[];
}

interface AlarmGroupsProps {
	groups: AlarmGroup[];
	expandedGroups: Set<string>;
	onToggleGroup: (groupKey: string) => void;
	onDeleteAlarm: (alarm: Alarm) => void;
	onEditName: (type: "room" | "user", id: string, currentName: string) => void;
	visibleGroupCount: number;
	onLoadMore: () => void;
	isDeleting: boolean;
}

export const AlarmGroups = ({
	groups,
	expandedGroups,
	onToggleGroup,
	onDeleteAlarm,
	onEditName,
	visibleGroupCount,
	onLoadMore,
	isDeleting,
}: AlarmGroupsProps) => {
	const visibleGroups = groups.slice(0, visibleGroupCount);
	const groupRows = chunkAlarmGroups(visibleGroups);

	return (
		<>
			{groups.length === 0 ? (
				<div className="py-16 flex flex-col items-center text-center bg-slate-50/50 rounded-2xl border border-dashed border-slate-200">
					<div className="flex items-center justify-center w-16 h-16 rounded-full bg-slate-100 text-slate-400 mb-4 ring-4 ring-white shadow-sm">
						<Bell size={32} aria-hidden="true" />
					</div>
					<h3 className="text-lg font-bold text-slate-800 tracking-tight">
						알람이 없습니다
					</h3>
					<p className="text-sm text-slate-500 mt-1">
						새로운 알람을 등록하거나 검색어를 변경해보세요.
					</p>
				</div>
			) : (
				<div className="space-y-6">
					<VirtualList
						items={groupRows}
						estimateSize={() => 360}
						recomputeKey={expandedGroups}
						className="max-h-[70vh] pr-2 pb-2 custom-scrollbar"
						itemClassName="pb-6"
						renderItem={(row) => (
							<div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6 items-start">
								{row.map((group) => {
									const groupKey = `${group.roomId}:${group.userId}`;
									const isExpanded = expandedGroups.has(groupKey);
									const displayAlarms = isExpanded
										? group.alarms
										: group.alarms.slice(0, GROUP_PREVIEW_COUNT);
									const hasMore = group.alarms.length > GROUP_PREVIEW_COUNT;

									return (
										<div
											key={groupKey}
											role="listitem"
											className="flex flex-col bg-white border border-slate-200/80 rounded-2xl overflow-hidden shadow-sm hover:shadow-xl hover:border-sky-200/50 transition-all duration-300 focus-within:border-sky-500 focus-within:ring-1 focus-within:ring-sky-500"
										>
											<div
												role="button"
												tabIndex={0}
												aria-expanded={isExpanded}
												onClick={() => {
													onToggleGroup(groupKey);
												}}
												onKeyDown={(event) => {
													if (event.key === "Enter" || event.key === " ") {
														event.preventDefault();
														onToggleGroup(groupKey);
													}
												}}
												className="group bg-slate-50/50 px-5 py-4 cursor-pointer hover:bg-sky-50/50 transition-colors border-b border-slate-100 outline-none focus-visible:bg-sky-50"
											>
												<div className="flex items-center justify-between mb-3">
													<div className="flex items-center gap-3">
														<div className="flex items-center justify-center w-8 h-8 rounded-full bg-indigo-100 text-indigo-600 font-black text-sm ring-2 ring-white shadow-sm">
															{group.roomName ? group.roomName[0] : "?"}
														</div>
														<span className="text-xs font-bold text-slate-500 bg-white px-2.5 py-1 rounded-md border border-slate-200 shadow-sm tabular-nums">
															총 {numberFormatter.format(group.alarms.length)}개
														</span>
													</div>
													<div className="w-8 h-8 flex items-center justify-center rounded-full bg-white border border-slate-200 text-slate-400 group-hover:text-sky-600 group-hover:border-sky-200 shadow-sm transition-all">
														{isExpanded ? (
															<ChevronUp size={18} aria-hidden="true" />
														) : (
															<ChevronDown size={18} aria-hidden="true" />
														)}
													</div>
												</div>

												<div className="space-y-2">
													<div className="flex items-center justify-between gap-2 group/edit">
														<Badge
															variant="outline"
															className="bg-sky-50 text-sky-700 border-sky-200/60 gap-1.5 py-1 font-semibold flex-1 justify-start truncate"
														>
															<MapPin
																size={14}
																className="text-sky-500"
																aria-hidden="true"
															/>
															<span className="truncate">{group.roomName}</span>
														</Badge>
														<Button
															variant="ghost"
															size="icon"
															className="h-7 w-7 text-slate-300 hover:text-sky-600 hover:bg-sky-100 focus-visible:ring-2 focus-visible:ring-sky-500 transition-colors"
															onClick={(event) => {
																event.stopPropagation();
																onEditName("room", group.roomId, group.roomName);
															}}
															aria-label={`${group.roomName} 방 이름 수정`}
														>
															<Edit2 size={14} aria-hidden="true" />
														</Button>
													</div>

													<div className="flex items-center justify-between gap-2 group/edit">
														<Badge
															variant="outline"
															className="bg-indigo-50 text-indigo-700 border-indigo-200/60 gap-1.5 py-1 font-semibold flex-1 justify-start truncate"
														>
															<User
																size={14}
																className="text-indigo-500"
																aria-hidden="true"
															/>
															<span className="truncate">{group.userName}</span>
														</Badge>
														<Button
															variant="ghost"
															size="icon"
															className="h-7 w-7 text-slate-300 hover:text-indigo-600 hover:bg-indigo-100 focus-visible:ring-2 focus-visible:ring-indigo-500 transition-colors"
															onClick={(event) => {
																event.stopPropagation();
																onEditName("user", group.userId, group.userName);
															}}
															aria-label={`${group.userName} 유저 이름 수정`}
														>
															<Edit2 size={14} aria-hidden="true" />
														</Button>
													</div>
												</div>
											</div>

											<div
												className="divide-y divide-slate-100 flex-1 flex flex-col bg-white"
												role="list"
											>
												{displayAlarms.map((alarm, index) => (
													<div
														key={`${alarm.channelId}-${String(index)}`}
														role="listitem"
														className="px-5 py-3.5 hover:bg-slate-50 flex items-center justify-between group/alarm transition-colors"
													>
														<div className="flex items-center gap-3.5 min-w-0">
															<div
																className="h-9 w-9 rounded-full bg-slate-100 flex items-center justify-center text-slate-600 font-black text-sm ring-2 ring-white shadow-sm shrink-0"
																aria-hidden="true"
															>
																{alarm.memberName ? alarm.memberName[0] : "?"}
															</div>
															<div className="truncate">
																<div className="font-bold text-slate-800 text-sm truncate group-hover/alarm:text-sky-700 transition-colors">
																	{alarm.memberName || "이름 없음"}
																</div>
																<div className="text-[11px] text-slate-400 font-mono mt-0.5 truncate tracking-tight">
																	{alarm.channelId}
																</div>
															</div>
														</div>
														<Button
															variant="ghost"
															size="sm"
															onClick={(event) => {
																event.stopPropagation();
																onDeleteAlarm(alarm);
															}}
															disabled={isDeleting}
															className="text-red-400 hover:text-red-600 hover:bg-red-50 opacity-0 group-hover/alarm:opacity-100 focus-visible:opacity-100 transition-all focus-visible:ring-2 focus-visible:ring-red-500 h-8 w-8 p-0 shrink-0 ml-2"
															aria-label={`${alarm.memberName || "알 수 없는 멤버"} 알람 삭제`}
														>
															<Trash2 size={16} aria-hidden="true" />
														</Button>
													</div>
												))}

												{!isExpanded && hasMore && (
													<div className="mt-auto bg-slate-50/50 p-3 text-center border-t border-slate-100">
														<button
															onClick={(event) => {
																event.stopPropagation();
																onToggleGroup(groupKey);
															}}
															className="inline-flex items-center justify-center text-xs font-bold text-slate-500 hover:text-sky-600 bg-white border border-slate-200 px-4 py-1.5 rounded-full shadow-sm hover:shadow transition-all focus-visible:ring-2 focus-visible:ring-sky-500 outline-none"
														>
															+{" "}
															{numberFormatter.format(
																group.alarms.length - displayAlarms.length,
															)}
															개 더보기
														</button>
													</div>
												)}
											</div>
										</div>
									);
								})}
							</div>
						)}
					/>

					{visibleGroupCount < groups.length && (
						<div className="flex justify-center pb-6">
							<Button
								variant="secondary"
								onClick={onLoadMore}
								className="px-6 py-2.5 font-bold shadow-sm hover:shadow transition-all active:scale-95 focus-visible:ring-2 focus-visible:ring-sky-500 focus-visible:outline-none"
							>
								그룹 더 보기
							</Button>
						</div>
					)}
				</div>
			)}
		</>
	);
};
