import Calendar from "lucide-react/dist/esm/icons/calendar";
import CheckSquare from "lucide-react/dist/esm/icons/check-square";
import Clock from "lucide-react/dist/esm/icons/clock";
import CloudSun from "lucide-react/dist/esm/icons/cloud-sun";
import FileText from "lucide-react/dist/esm/icons/file-text";
import Link2 from "lucide-react/dist/esm/icons/link-2";
import Link2Off from "lucide-react/dist/esm/icons/link-2-off";
import Power from "lucide-react/dist/esm/icons/power";
import Trash2 from "lucide-react/dist/esm/icons/trash-2";
import { Badge, Button, Card } from "@/components/ui";

type AssistantUser = {
	id: string;
	internalId: string;
	isConnected: boolean;
	lastActivity: string;
	stats: {
		memos: number;
		todos: number;
		reminders: number;
	};
	features: {
		morningBriefing: boolean;
		weatherAlerts: boolean;
		reminders: boolean;
	};
};

type AssistantUserCardProps = {
	user: AssistantUser;
	onToggleFeature: (
		userId: string,
		feature: "morningBriefing" | "weatherAlerts" | "reminders",
		enabled: boolean,
	) => void;
	onRevokeToken: (userId: string) => void;
};

function formatRelativeTime(dateString: string): string {
	const date = new Date(dateString);
	const now = new Date();
	const diffInSeconds = Math.floor((now.getTime() - date.getTime()) / 1000);

	if (diffInSeconds < 60) return "방금";
	if (diffInSeconds < 3600)
		return `${String(Math.floor(diffInSeconds / 60))}분 전`;
	if (diffInSeconds < 86400)
		return `${String(Math.floor(diffInSeconds / 3600))}시간 전`;
	if (diffInSeconds < 2592000)
		return `${String(Math.floor(diffInSeconds / 86400))}일 전`;

	return date.toLocaleDateString("ko-KR");
}

const numberFormatter = new Intl.NumberFormat("ko-KR");

const AssistantUserCard = ({
	user,
	onToggleFeature,
	onRevokeToken,
}: AssistantUserCardProps) => (
	<Card className="relative group overflow-hidden border-slate-200 [content-visibility:auto] contain-intrinsic-size-[400px] focus-within:ring-2 focus-within:ring-sky-100 transition-shadow">
		<Card.Header className="pb-3 border-b border-slate-50">
			<div className="flex items-start justify-between">
				<div>
					<div className="flex items-center gap-2 mb-1">
						<span className="text-xs font-mono text-slate-400">
							ID: {user.id}
						</span>
					</div>
					<div className="flex items-center gap-2">
						<h3
							className="font-bold text-sm text-slate-800 font-mono select-all"
							title={user.internalId}
						>
							{user.internalId.slice(0, 8)}…
						</h3>
						{user.isConnected ? (
							<Badge
								color="sky"
								className="text-[10px] px-1.5 py-0.5 shadow-none ring-1 ring-sky-200 gap-1"
							>
								<Link2 size={10} aria-hidden="true" /> 연결됨
							</Badge>
						) : (
							<Badge
								color="gray"
								className="text-[10px] px-1.5 py-0.5 shadow-none ring-1 ring-slate-200 gap-1"
							>
								<Link2Off size={10} aria-hidden="true" /> 연결 해제
							</Badge>
						)}
					</div>
				</div>

				<div className="text-right">
					<div className="text-[10px] text-slate-400 font-bold uppercase tracking-tight">
						마지막 활동
					</div>
					<div className="text-xs font-medium text-slate-600">
						{user.lastActivity ? formatRelativeTime(user.lastActivity) : "-"}
					</div>
				</div>
			</div>
		</Card.Header>

		<Card.Body className="space-y-4 pt-3">
			<div className="grid grid-cols-3 gap-2">
				<div className="bg-slate-50 p-2 rounded-lg text-center border border-slate-100/50">
					<div className="text-slate-400 mb-1 flex justify-center">
						<FileText size={14} aria-hidden="true" />
					</div>
					<div className="text-lg font-bold text-slate-700 tabular-nums">
						{numberFormatter.format(user.stats.memos)}
					</div>
					<div className="text-[10px] text-slate-500 font-medium">메모</div>
				</div>
				<div className="bg-slate-50 p-2 rounded-lg text-center border border-slate-100/50">
					<div className="text-slate-400 mb-1 flex justify-center">
						<CheckSquare size={14} aria-hidden="true" />
					</div>
					<div className="text-lg font-bold text-slate-700 tabular-nums">
						{numberFormatter.format(user.stats.todos)}
					</div>
					<div className="text-[10px] text-slate-500 font-medium">할 일</div>
				</div>
				<div className="bg-slate-50 p-2 rounded-lg text-center border border-slate-100/50">
					<div className="text-slate-400 mb-1 flex justify-center">
						<Clock size={14} aria-hidden="true" />
					</div>
					<div className="text-lg font-bold text-slate-700 tabular-nums">
						{numberFormatter.format(user.stats.reminders)}
					</div>
					<div className="text-[10px] text-slate-500 font-medium">리마인더</div>
				</div>
			</div>

			<div>
				<div className="text-[11px] font-bold text-slate-400 uppercase tracking-wider mb-2 flex items-center gap-1">
					<span
						className="w-1.5 h-1.5 rounded-full bg-amber-400"
						aria-hidden="true"
					></span>
					자동 알림 기능
				</div>
				<div className="space-y-2">
					<button
						onClick={() => {
							onToggleFeature(
								user.internalId,
								"morningBriefing",
								!user.features.morningBriefing,
							);
						}}
						className={`w-full flex items-center justify-between p-2.5 rounded-lg text-sm transition-all border outline-none focus-visible:ring-2 ${
							user.features.morningBriefing
								? "bg-amber-50 border-amber-200 text-amber-900 focus-visible:ring-amber-200"
								: "bg-white border-slate-200 text-slate-500 hover:bg-slate-50 focus-visible:ring-slate-200"
						}`}
						aria-label={`아침 브리핑 ${user.features.morningBriefing ? "끄기" : "켜기"}`}
					>
						<div className="flex items-center gap-2">
							<CloudSun
								size={16}
								className={
									user.features.morningBriefing
										? "text-amber-500"
										: "text-slate-400"
								}
								aria-hidden="true"
							/>
							<span className="font-medium">아침 브리핑</span>
						</div>
						<Power
							size={14}
							className={
								user.features.morningBriefing
									? "text-amber-500"
									: "text-slate-300"
							}
							aria-hidden="true"
						/>
					</button>

					<button
						onClick={() => {
							onToggleFeature(
								user.internalId,
								"weatherAlerts",
								!user.features.weatherAlerts,
							);
						}}
						className={`w-full flex items-center justify-between p-2.5 rounded-lg text-sm transition-all border outline-none focus-visible:ring-2 ${
							user.features.weatherAlerts
								? "bg-sky-50 border-sky-200 text-sky-900 focus-visible:ring-sky-200"
								: "bg-white border-slate-200 text-slate-500 hover:bg-slate-50 focus-visible:ring-slate-200"
						}`}
						aria-label={`날씨 알림 ${user.features.weatherAlerts ? "끄기" : "켜기"}`}
					>
						<div className="flex items-center gap-2">
							<CloudSun
								size={16}
								className={
									user.features.weatherAlerts
										? "text-sky-500"
										: "text-slate-400"
								}
								aria-hidden="true"
							/>
							<span className="font-medium">날씨 알림</span>
						</div>
						<Power
							size={14}
							className={
								user.features.weatherAlerts ? "text-sky-500" : "text-slate-300"
							}
							aria-hidden="true"
						/>
					</button>

					<button
						onClick={() => {
							onToggleFeature(
								user.internalId,
								"reminders",
								!user.features.reminders,
							);
						}}
						className={`w-full flex items-center justify-between p-2.5 rounded-lg text-sm transition-all border outline-none focus-visible:ring-2 ${
							user.features.reminders
								? "bg-indigo-50 border-indigo-200 text-indigo-900 focus-visible:ring-indigo-200"
								: "bg-white border-slate-200 text-slate-500 hover:bg-slate-50 focus-visible:ring-slate-200"
						}`}
						aria-label={`캘린더/리마인더 ${user.features.reminders ? "끄기" : "켜기"}`}
					>
						<div className="flex items-center gap-2">
							<Calendar
								size={16}
								className={
									user.features.reminders ? "text-indigo-500" : "text-slate-400"
								}
								aria-hidden="true"
							/>
							<span className="font-medium">캘린더/리마인더</span>
						</div>
						<Power
							size={14}
							className={
								user.features.reminders ? "text-indigo-500" : "text-slate-300"
							}
							aria-hidden="true"
						/>
					</button>
				</div>
			</div>
		</Card.Body>

		<Card.Footer className="border-t border-slate-50 pt-3">
			{user.isConnected && (
				<Button
					variant="destructive"
					size="sm"
					className="w-full text-xs h-8 shadow-sm shadow-rose-100 focus-visible:ring-2 focus-visible:ring-rose-200"
					onClick={() => {
						onRevokeToken(user.internalId);
					}}
					aria-label="Google 토큰 해제"
				>
					<Trash2 size={12} className="mr-1.5" aria-hidden="true" /> Google 토큰
					해제
				</Button>
			)}
		</Card.Footer>
	</Card>
);

export default AssistantUserCard;
