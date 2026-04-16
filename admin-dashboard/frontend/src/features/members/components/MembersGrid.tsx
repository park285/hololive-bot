import MemberCard from "@/components/MemberCard";
import { Button } from "@/components/ui/Button";
import { VirtualList } from "@/components/ui/VirtualList";
import type { Member } from "@/features/members/types";

const MEMBERS_GRID_ROW_SIZE = 4;

const chunkMembers = (members: Member[]) => {
	const rows: Member[][] = [];
	for (let index = 0; index < members.length; index += MEMBERS_GRID_ROW_SIZE) {
		rows.push(members.slice(index, index + MEMBERS_GRID_ROW_SIZE));
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
}: MembersGridProps) => (
	<>
		{totalCount === 0 ? (
			<div className="py-12 text-center text-slate-400 bg-slate-50 rounded-2xl border border-dashed border-slate-200">
				검색 결과가 없습니다.
			</div>
		) : (
			<VirtualList
				items={chunkMembers(visibleMembers)}
				estimateSize={() => 380}
				className="max-h-[70vh] pr-1"
				itemClassName="pb-5"
				renderItem={(row, rowIndex) => (
					<div
						className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5"
						role="list"
					>
						{row.map((member, columnIndex) => {
							const memberIndex = rowIndex * MEMBERS_GRID_ROW_SIZE + columnIndex;
							return (
								<div
									key={`${String(member.id)}-${String(memberIndex)}`}
									role="listitem"
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
		)}

		{canLoadMore && (
			<div className="flex justify-center">
				<Button variant="secondary" onClick={onLoadMore} className="px-5">
					멤버 더 보기
				</Button>
			</div>
		)}
	</>
);
