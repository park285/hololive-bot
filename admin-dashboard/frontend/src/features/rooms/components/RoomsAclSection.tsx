import clsx from "clsx";
import Shield from "lucide-react/dist/esm/icons/shield.mjs";
import ShieldAlert from "lucide-react/dist/esm/icons/shield-alert.mjs";
import ShieldBan from "lucide-react/dist/esm/icons/shield-ban.mjs";
import { Card } from "@/components/ui/Card";
import type { ACLMode } from "@/features/rooms/types";

interface RoomsAclSectionProps {
	aclEnabled: boolean;
	aclMode: ACLMode;
	description: string;
	isPending: boolean;
	onToggleACL: () => void;
	onModeChange: (mode: ACLMode) => void;
}

export const RoomsAclSection = ({
	aclEnabled,
	aclMode,
	description,
	isPending,
	onToggleACL,
	onModeChange,
}: RoomsAclSectionProps) => {
	const isBlacklist = aclMode === "blacklist";

	return (
		<Card
			className={clsx(
				"relative transition-all duration-300 border overflow-hidden",
				aclEnabled
					? "bg-card border-blue-100 shadow-sm"
					: "bg-muted border-border",
			)}
		>
			<div className="p-6 space-y-5">
			<div className={clsx("absolute top-0 left-0 right-0 h-1", !aclEnabled ? "bg-linear-to-r from-slate-300 to-slate-400" : isBlacklist ? "bg-linear-to-r from-rose-400 to-rose-500" : "bg-linear-to-r from-sky-400 to-cyan-400")} />
				<div className="flex flex-col md:flex-row items-center justify-between gap-4">
					<div className="flex items-start gap-4">
						<div
							className={clsx(
								"p-3 rounded-full mt-1 transition-colors",
								aclEnabled
									? isBlacklist
										? "bg-rose-50"
										: "bg-blue-50"
									: "bg-border",
							)}
							aria-hidden="true"
						>
							{!aclEnabled ? (
								<ShieldAlert className="text-muted-foreground" size={24} />
							) : isBlacklist ? (
								<ShieldBan className="text-rose-600" size={24} />
							) : (
								<Shield className="text-blue-600" size={24} />
							)}
						</div>
						<div>
							<h3 className="text-lg font-display font-bold text-foreground mb-1">
								방 접근 제어 (ACL)
							</h3>
							<p className="text-sm text-muted-foreground max-w-lg leading-relaxed">
								{description}
							</p>
						</div>
					</div>

					<div className="flex items-center gap-3">
						<span
							className={clsx(
								"text-sm font-bold",
								aclEnabled ? "text-blue-600" : "text-muted-foreground",
							)}
						>
							{aclEnabled ? "활성화됨" : "비활성화됨"}
						</span>
						<button
							onClick={onToggleACL}
							disabled={isPending}
							role="switch"
							aria-checked={aclEnabled}
							aria-label="방 접근 제어 토글"
							className={clsx(
								"relative inline-flex h-7 w-12 items-center rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500",
								aclEnabled
								? "bg-linear-to-r from-sky-500 to-cyan-500"
								: "bg-slate-300 dark:bg-slate-600",
								isPending && "opacity-50 cursor-wait",
							)}
						>
							<span
								className={clsx(
									"inline-block h-5 w-5 transform rounded-full bg-card shadow transition-transform",
									aclEnabled ? "translate-x-6" : "translate-x-1",
								)}
							/>
						</button>
					</div>
				</div>

				{aclEnabled && (
					<div className="flex flex-wrap items-center gap-3 pt-3 border-t border-border-subtle">
						<span className="text-sm font-semibold text-muted-foreground mr-1">
							모드
						</span>
						<div
							className="inline-flex rounded-lg bg-muted p-0.5"
							role="radiogroup"
							aria-label="ACL 모드 선택"
						>
							<button
								role="radio"
								aria-checked={!isBlacklist}
								onClick={() => {
									onModeChange("whitelist");
								}}
								disabled={isPending}
								className={clsx(
									"px-4 py-1.5 rounded-md text-sm font-semibold transition-all",
									!isBlacklist
										? "bg-card text-blue-700 shadow-sm"
										: "text-muted-foreground hover:text-foreground",
									isPending && "opacity-50 cursor-wait",
								)}
							>
								화이트리스트
							</button>
							<button
								role="radio"
								aria-checked={isBlacklist}
								onClick={() => {
									onModeChange("blacklist");
								}}
								disabled={isPending}
								className={clsx(
									"px-4 py-1.5 rounded-md text-sm font-semibold transition-all",
									isBlacklist
										? "bg-card text-rose-700 shadow-sm"
										: "text-muted-foreground hover:text-foreground",
									isPending && "opacity-50 cursor-wait",
								)}
							>
								블랙리스트
							</button>
						</div>
					</div>
				)}
			</div>
		</Card>
	);
};
