import { useQuery } from "@tanstack/react-query";
import {
	useCallback,
	useDeferredValue,
	useEffect,
	useMemo,
	useState,
} from "react";
import { queryKeys } from "@/queries/keys";
import { membersApi } from "@/features/members/api";
import {
	cloneMembers,
	filterMembers,
	sortMembers,
} from "@/features/members/selectors";
import { useMemberMutations } from "@/features/members/hooks/mutations";
import { queryView } from "@/queries/state";
import { useOnline } from "@/queries/useOnline";
import { RequestBlockedError } from "@/api/errors";

const MEMBER_PAGE_SIZE = 48;

export type MembersModalState =
	| { type: "none" }
	| {
			type: "removeAlias";
			memberId: string;
			aliasType: "ko" | "ja";
			alias: string;
	  }
	| {
			type: "graduation";
			memberId: string;
			memberName: string;
			currentStatus: boolean;
	  }
	| {
			type: "channelEdit";
			memberId: string;
			memberName: string;
			currentChannelId: string;
	  }
	| { type: "nameEdit"; memberId: string; currentName: string };

export function useMembersPage() {
	const query = useQuery({
		queryKey: queryKeys.members.all,
		queryFn: membersApi.getAll,
	});

	const view = queryView(query, useOnline(), data => data.members.length === 0);
	const mutations = useMemberMutations();
	const [searchTerm, setSearchTerm] = useState("");
	const deferredSearchTerm = useDeferredValue(searchTerm);
	const [hideGraduated, setHideGraduated] = useState<boolean>(() => {
		const saved = localStorage.getItem("hideGraduated");
		return saved !== null ? saved === "true" : true;
	});
	const [modal, setModal] = useState<MembersModalState>({ type: "none" });
	const [addModal, setAddModal] = useState<object | null>(null);
	const [visibleCount, setVisibleCount] = useState(MEMBER_PAGE_SIZE);

	const allMembers = useMemo(
		() => cloneMembers(query.data?.members ?? []),
		[query.data?.members],
	);
	const toggleHideGraduated = () => {
		const nextValue = !hideGraduated;
		setHideGraduated(nextValue);
		localStorage.setItem("hideGraduated", String(nextValue));
	};

	useEffect(() => {
		setVisibleCount(MEMBER_PAGE_SIZE);
	}, [deferredSearchTerm, hideGraduated]);

	const assertCurrent = () => {
		if (!view.current) throw new RequestBlockedError("CLIENT_NOT_READY", "최신 멤버 목록을 먼저 조회해 주세요.");
	};
	const handleAddAlias = async (memberId: string, type: "ko" | "ja", rawAlias: string) => {
		assertCurrent();
		const alias = rawAlias.trim();
		if (alias) await mutations.addAlias.mutateAsync({ memberId, type, alias });
	};
	const handleRemoveAlias = useCallback((memberId: string, type: "ko" | "ja", alias: string) => {
		setModal({ type: "removeAlias", memberId, aliasType: type, alias });
	}, []);
	const confirmRemoveAlias = () => {
		if (modal.type !== "removeAlias" || !view.current) return;
		mutations.removeAlias.mutate({ memberId: modal.memberId, type: modal.aliasType, alias: modal.alias });
		setModal({ type: "none" });
	};
	const handleUpdateChannel = useCallback((memberId: string, memberName: string, currentChannelId: string) => {
		setModal({ type: "channelEdit", memberId, memberName, currentChannelId });
	}, []);
	const confirmUpdateChannel = async (channelId: string) => {
		assertCurrent();
		if (modal.type !== "channelEdit") return;
		await mutations.updateChannel.mutateAsync({ memberId: modal.memberId, channelId });
		setModal(current => current === modal ? { type: "none" } : current);
	};
	const handleEditName = useCallback((memberId: string, currentName: string) => {
		setModal({ type: "nameEdit", memberId, currentName });
	}, []);
	const confirmEditName = async (name: string) => {
		assertCurrent();
		if (modal.type !== "nameEdit") return;
		await mutations.updateName.mutateAsync({ memberId: modal.memberId, name });
		setModal(current => current === modal ? { type: "none" } : current);
	};
	const handleToggleGraduation = useCallback((memberId: string, memberName: string, currentStatus: boolean) => {
		setModal({ type: "graduation", memberId, memberName, currentStatus });
	}, []);
	const confirmToggleGraduation = () => {
		if (modal.type !== "graduation" || !view.current) return;
		const latest = allMembers.find(member => member.id === modal.memberId);
		if (latest?.isGraduated !== modal.currentStatus) return;
		mutations.setGraduation.mutate({ memberId: modal.memberId, isGraduated: !modal.currentStatus });
		setModal({ type: "none" });
	};

	const filteredMembers = useMemo(
		() => filterMembers(allMembers, deferredSearchTerm, hideGraduated),
		[deferredSearchTerm, hideGraduated, allMembers],
	);
	const sortedMembers = useMemo(
		() => sortMembers(filteredMembers),
		[filteredMembers],
	);
	const visibleMembers = useMemo(
		() => sortedMembers.slice(0, visibleCount),
		[sortedMembers, visibleCount],
	);

	return {
		query,
		view,
		mutations,
		searchTerm,
		setSearchTerm,
		hideGraduated,
		toggleHideGraduated,
		modal,
		setModal,
		addModal,
		setAddModal,
		visibleCount,
		setVisibleCount,
		allMembers,
		sortedMembers,
		visibleMembers,
		handleAddAlias,
		handleRemoveAlias,
		confirmRemoveAlias,
		handleUpdateChannel,
		confirmUpdateChannel,
		handleEditName,
		confirmEditName,
		handleToggleGraduation,
		confirmToggleGraduation,
	};
}
