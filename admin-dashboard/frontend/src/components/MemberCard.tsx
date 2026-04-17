import Edit2 from "lucide-react/dist/esm/icons/edit-2";
import ExternalLink from "lucide-react/dist/esm/icons/external-link";
import GraduationCap from "lucide-react/dist/esm/icons/graduation-cap";
import Plus from "lucide-react/dist/esm/icons/plus";
import RotateCcw from "lucide-react/dist/esm/icons/rotate-ccw";
import { memo, useState } from "react";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Input } from "@/components/ui/Input";
import type { Member } from "@/features/members/types";

type MemberCardProps = {
	member: Member;
	onAddAlias: (memberId: number, type: "ko" | "ja", alias: string) => void;
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
};

const MemberCard = memo(
	({
		member,
		onAddAlias,
		onRemoveAlias,
		onToggleGraduation,
		onEditChannel,
		onEditName,
	}: MemberCardProps) => {
		const [koInput, setKoInput] = useState("");
		const [jaInput, setJaInput] = useState("");

		const koAliases = member.aliases.ko;
		const jaAliases = member.aliases.ja;

		const handleAddKoAlias = () => {
			const alias = koInput.trim();
			if (!alias) return;

			onAddAlias(member.id, "ko", alias);
			setKoInput("");
		};

		const handleAddJaAlias = () => {
			const alias = jaInput.trim();
			if (!alias) return;

			onAddAlias(member.id, "ja", alias);
			setJaInput("");
		};

		return (
			<Card className="relative group flex flex-col h-full overflow-hidden border-slate-200 [content-visibility:auto] contain-intrinsic-size-[350px] focus-within:ring-2 focus-within:ring-sky-100 transition-shadow">
				<Card.Header className="pb-3 border-b border-slate-50">
					<div className="flex items-start justify-between">
						<div>
							<div className="mb-1 flex items-center gap-2">
								<span className="text-xs font-mono text-slate-400">
									#{String(member.id).padStart(3, "0")}
								</span>
								{member.isGraduated && (
									<Badge
										color="gray"
										className="text-[10px] px-1.5 py-0.5 shadow-none ring-1 ring-slate-200"
									>
										졸업 멤버
									</Badge>
								)}
							</div>
							<div className="flex items-center gap-1.5">
								<h3 className="text-lg font-display font-bold leading-tight text-slate-800">
									{member.name}
								</h3>
								<button
									onClick={() => {
										onEditName(member.id, member.name);
									}}
									className="rounded p-1 text-slate-400 opacity-0 transition-all hover:text-sky-600 group-hover:opacity-100 outline-none focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-sky-200"
									title="이름 수정"
									aria-label={`${member.name} 이름 수정`}
								>
									<Edit2 size={14} aria-hidden="true" />
								</button>
							</div>
						</div>

						<button
							onClick={() => {
								onToggleGraduation(member.id, member.name, member.isGraduated);
							}}
							className={`rounded-lg p-2 outline-none transition-all focus-visible:ring-2 ${
								member.isGraduated
									? "text-slate-400 hover:bg-emerald-50 hover:text-emerald-600 focus-visible:ring-emerald-200"
									: "text-slate-300 hover:bg-rose-50 hover:text-rose-600 focus-visible:ring-rose-200"
							}`}
							title={member.isGraduated ? "졸업 해제 (복귀)" : "졸업 처리"}
							aria-label={
								member.isGraduated
									? `${member.name} 졸업 해제 및 복귀`
									: `${member.name} 졸업 처리`
							}
						>
							{member.isGraduated ? (
								<RotateCcw size={18} aria-hidden="true" />
							) : (
								<GraduationCap size={18} aria-hidden="true" />
							)}
						</button>
					</div>

					<div className="mt-3 flex items-center gap-2 rounded-lg bg-slate-50 p-2 text-xs text-slate-500">
						<span
							className="flex-1 truncate font-mono select-all"
							title={member.channelId}
						>
							{member.channelId}
						</span>
						<button
							onClick={(event) => {
								event.stopPropagation();
								onEditChannel(member.id, member.name, member.channelId);
							}}
							className="rounded p-1 text-sky-600 shadow-sm outline-none transition-colors hover:bg-white focus-visible:ring-2 focus-visible:ring-sky-200"
							title="채널 ID 수정"
							aria-label={`${member.name} 채널 ID 수정`}
						>
							<Edit2 size={12} aria-hidden="true" />
						</button>
						<a
							href={`https://youtube.com/channel/${member.channelId}`}
							target="_blank"
							rel="noopener noreferrer"
							className="rounded p-1 text-slate-400 shadow-sm outline-none transition-colors hover:bg-white hover:text-red-500 focus-visible:ring-2 focus-visible:ring-red-200"
							title="유튜브 채널 이동"
							aria-label={`${member.name} 유튜브 채널로 이동`}
						>
							<ExternalLink size={12} aria-hidden="true" />
						</a>
					</div>
				</Card.Header>

				<Card.Body className="space-y-4 pt-2 flex-1 flex flex-col">
					<section aria-labelledby={`ko-aliases-${String(member.id)}`}>
						<div
							id={`ko-aliases-${String(member.id)}`}
							className="mb-2 flex items-center gap-1 text-[11px] font-bold uppercase tracking-wider text-slate-400"
						>
							<span
								className="h-1.5 w-1.5 rounded-full bg-sky-400"
								aria-hidden="true"
							></span>
							한국어 별명
						</div>
						<div
							className="mb-2 flex min-h-[24px] flex-wrap gap-1.5"
							role="list"
						>
							{koAliases.map((alias: string) => (
								<Badge
									key={alias}
									color="sky"
									onRemove={() => {
										onRemoveAlias(member.id, "ko", alias);
									}}
									aria-label={`${alias} 별명 제거`}
									role="listitem"
								>
									{alias}
								</Badge>
							))}
							{koAliases.length === 0 && (
								<span className="text-xs italic text-slate-300">
									등록된 별명이 없습니다
								</span>
							)}
						</div>
						<div className="flex gap-1.5">
							<Input
								value={koInput}
								onChange={(event) => {
									setKoInput(event.target.value);
								}}
								placeholder="별명 추가…"
								className="h-8 flex-1 border-slate-200 bg-slate-50 text-xs focus-visible:ring-2 focus-visible:ring-sky-200"
								onKeyDown={(event) => {
									if (event.key === "Enter") handleAddKoAlias();
								}}
								aria-label={`${member.name} 한국어 별명 입력`}
							/>
							<Button
								variant="primary"
								size="sm"
								onClick={handleAddKoAlias}
								className="flex h-8 w-8 items-center justify-center bg-sky-500 p-0 hover:bg-sky-600 focus-visible:ring-2 focus-visible:ring-sky-200"
								aria-label="한국어 별명 추가"
							>
								<Plus size={14} aria-hidden="true" />
							</Button>
						</div>
					</section>

					<section aria-labelledby={`ja-aliases-${String(member.id)}`}
						className="flex-1"
					>
						<div
							id={`ja-aliases-${String(member.id)}`}
							className="mb-2 flex items-center gap-1 text-[11px] font-bold uppercase tracking-wider text-slate-400"
						>
							<span
								className="h-1.5 w-1.5 rounded-full bg-rose-400"
								aria-hidden="true"
							></span>
							일본어 별명
						</div>
						<div
							className="mb-2 flex min-h-[24px] flex-wrap gap-1.5"
							role="list"
						>
							{jaAliases.map((alias: string) => (
								<Badge
									key={alias}
									color="rose"
									onRemove={() => {
										onRemoveAlias(member.id, "ja", alias);
									}}
									aria-label={`${alias} 별명 제거`}
									role="listitem"
								>
									{alias}
								</Badge>
							))}
							{jaAliases.length === 0 && (
								<span className="text-xs italic text-slate-300">
									등록된 별명이 없습니다
								</span>
							)}
						</div>
						<div className="flex gap-1.5">
							<Input
								value={jaInput}
								onChange={(event) => {
									setJaInput(event.target.value);
								}}
								placeholder="일본어 별명 추가…"
								className="h-8 flex-1 border-slate-200 bg-slate-50 text-xs focus-visible:ring-2 focus-visible:ring-rose-200"
								onKeyDown={(event) => {
									if (event.key === "Enter") handleAddJaAlias();
								}}
								aria-label={`${member.name} 일본어 별명 입력`}
							/>
							<Button
								variant="primary"
								size="sm"
								onClick={handleAddJaAlias}
								className="flex h-8 w-8 items-center justify-center bg-rose-500 p-0 hover:bg-rose-600 focus-visible:ring-2 focus-visible:ring-rose-200"
								aria-label="일본어 별명 추가"
							>
								<Plus size={14} aria-hidden="true" />
							</Button>
						</div>
					</section>
				</Card.Body>
			</Card>
		);
	},
);

export default MemberCard;