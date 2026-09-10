import RefreshCw from "lucide-react/dist/esm/icons/refresh-cw.mjs";
import Server from "lucide-react/dist/esm/icons/server.mjs";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { QuerySection } from "@/components/ui/QuerySection";
import { VirtualList } from "@/components/ui/VirtualList";
import { ContainerConfirmation } from "@/features/docker/components/ContainerConfirmation";
import { ContainerItem } from "@/features/docker/components/ContainerItem";
import { useContainers } from "@/features/docker/hooks/useContainers";
import { QueryNotice } from "@/queries/QueryNotice";

export function ContainerList() {
	const { healthQuery, healthView, containersView, current, confirmation, canConfirm, open, close, confirm, refresh, actionInProgress } = useContainers();
	const availability = healthView.current ? healthView.data.available : undefined;
	return <>
		<Card className="relative overflow-hidden">
			<Card.Header className="flex flex-row flex-wrap items-center justify-between gap-2 border-b border-border-subtle pb-4">
				<div className="flex items-center gap-2"><Server size={18} aria-hidden="true" /><h3 className="text-lg font-bold text-foreground">컨테이너 관리</h3>
					<Badge color={availability === undefined ? "gray" : availability ? "green" : "rose"}>{availability === undefined ? "Docker 상태 미확인" : availability ? "Docker 연결됨" : "Docker 연결 안됨"}</Badge>
				</div>
				<Button type="button" variant="ghost" size="icon" onClick={refresh} disabled={healthView.fetching || containersView.fetching} aria-label="컨테이너 상태 새로고침"><RefreshCw size={18} aria-hidden="true" /></Button>
			</Card.Header>
			<Card.Body className="space-y-3 pt-6">
				<QueryNotice view={healthView} label="Docker 연결 상태" onRetry={() => { void healthQuery.refetch(); }} />
				{availability === false && <p role="status">Docker 서비스에 연결할 수 없습니다. 상태를 다시 조회해 주세요.</p>}
				{(availability === true || containersView.data !== undefined) && <QuerySection view={containersView} label="관리 대상 컨테이너" onRetry={refresh} emptyContent={<p role="status">관리 대상 컨테이너가 없습니다.</p>}>
					<fieldset disabled={!current} className="min-w-0">
						<VirtualList items={containersView.data?.containers ?? []} estimateSize={() => 108} getItemKey={container => container.id} recomputeKey={actionInProgress} className="max-h-[34rem] pr-1" itemClassName="pb-3"
							renderItem={container => <ContainerItem container={container} actionInProgress={actionInProgress} onAction={open} />} />
					</fieldset>
				</QuerySection>}
			</Card.Body>
		</Card>
		{confirmation && <ContainerConfirmation intent={confirmation} canConfirm={canConfirm} onClose={close} onConfirm={confirm} />}
	</>;
}
