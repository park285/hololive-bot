import MemberCard from "@/components/MemberCard";
import { Button } from "@/components/ui/Button";
import { VirtualList } from "@/components/ui/VirtualList";
import type { Member } from "@/features/members/types";

const MEMBER_GRID_COLUMNS = 4;

const chunkMembers = (members: Member[]) => {
	const rows: Member[][] = [];
	for (let index = 0; index < members.length; index += MEMBER_GRID_COLUMNS) {
		rows.push(members.slice(index, index + MEMBER_GRID_COLUMNS));
	}
	return rows;
};

interface MembersGridProps {
	visibleMembers: Member[];
	totalCount: number;
	canLoadMore: boolean;
	onLoadMore: () => void;
	onAddAlias: (memberId: number, type: "ko" | "ja", rawAlias: string) => void;
	onRemoveAlias: (memberId: number, type: "ko" | "ja", alias: string) => void;
	onToggleGraduation: (
		memberId: number,
		memberName: string,
		currentStatus: boolean,
	) => void;
	onEditChannel: (
		memberId: number,
		memberName: string,
		currentChannelId: string,
	) => void;
	onEditName: (memberId: number, currentName: string) => void;
}

export const MembersGrid = ({
	visibleMembers,
	totalCount,
	canLoadMore,
	onLoadMore,
	onAddAlias,
	onRemoveAlias,
	onToggleGraduation,
	onEditChannel,
	onEditName,
}: MembersGridProps) => {
	const memberRows = chunkMembers(visibleMembers);

	return (
		<>
			{totalCount === 0 ? (
				<div className="py-16 flex flex-col items-center text-center bg-slate-50/50 rounded-2xl border border-dashed border-slate-200">
					<p className="text-slate-400 font-medium">검색 결과가 없습니다.</p>
				</div>
			) : (
				<div className="space-y-6">
					<VirtualList
						items={memberRows}
						estimateSize={() => 370}
						className="max-h-[70vh] pr-2 pb-2 custom-scrollbar"
						itemClassName="pb-6"
						renderItem={(row, rowIndex) => (
							<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
								{row.map((member, columnIndex) => {
									const memberIndex = rowIndex * MEMBER_GRID_COLUMNS + columnIndex;

									return (
										<div
											key={`${String(member.id)}-${String(memberIndex)}`}
											role="listitem"
											className="h-full flex flex-col"
										>
											<MemberCard
												member={member}
												onAddAlias={onAddAlias}
												onRemoveAlias={onRemoveAlias}
												onToggleGraduation={onToggleGraduation}
												onEditChannel={onEditChannel}
												onEditName={onEditName}
											/>
										</div>
									);
								})}
							</div>
						)}
					/>
					{canLoadMore && (
						<div className="flex justify-center pb-6">
							<Button
								variant="secondary"
								onClick={onLoadMore}
								className="px-6 py-2.5 font-bold shadow-sm hover:shadow transition-all active:scale-95 focus-visible:ring-2 focus-visible:ring-sky-500 focus-visible:outline-none"
							>
								멤버 더 보기
							</Button>
						</div>
					)}
				</div>
			)}
		</>
	);
};
