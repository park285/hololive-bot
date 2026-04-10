import { useQuery } from "@tanstack/react-query";
import {
	startTransition,
	useCallback,
	useDeferredValue,
	useEffect,
	useMemo,
	useOptimistic,
	useState,
} from "react";
import { queryKeys } from "@/api/queryKeys";
import { membersApi } from "@/features/members/api";
import {
	cloneMembers,
	filterMembers,
	sortMembers,
} from "@/features/members/selectors";
import {
	optimisticMemberReducer,
	useMemberMutations,
} from "@/hooks/useMemberMutations";

const MEMBER_PAGE_SIZE = 48;

export type MembersModalState =
	| { type: "none" }
	| {
			type: "removeAlias";
			memberId: number;
			aliasType: "ko" | "ja";
			alias: string;
	  }
	| {
			type: "graduation";
			memberId: number;
			memberName: string;
			currentStatus: boolean;
	  }
	| {
			type: "channelEdit";
			memberId: number;
			memberName: string;
			currentChannelId: string;
	  }
	| { type: "nameEdit"; memberId: number; currentName: string };

export function useMembersPage() {
	const query = useQuery({
		queryKey: queryKeys.members.all,
		queryFn: membersApi.getAll,
	});

	const mutations = useMemberMutations();
	const [searchTerm, setSearchTerm] = useState("");
	const deferredSearchTerm = useDeferredValue(searchTerm);
	const [hideGraduated, setHideGraduated] = useState<boolean>(() => {
		const saved = localStorage.getItem("hideGraduated");
		return saved !== null ? saved === "true" : true;
	});
	const [modal, setModal] = useState<MembersModalState>({ type: "none" });
	const [isAddModalOpen, setIsAddModalOpen] = useState(false);
	const [visibleCount, setVisibleCount] = useState(MEMBER_PAGE_SIZE);

	const allMembers = useMemo(
		() => cloneMembers(query.data?.members ?? []),
		[query.data?.members],
	);
	const [optimisticMembers, setOptimisticMembers] = useOptimistic(
		allMembers,
		optimisticMemberReducer,
	);

	const toggleHideGraduated = () => {
		const nextValue = !hideGraduated;
		setHideGraduated(nextValue);
		localStorage.setItem("hideGraduated", String(nextValue));
	};

	useEffect(() => {
		setVisibleCount(MEMBER_PAGE_SIZE);
	}, [deferredSearchTerm, hideGraduated]);

	const handleAddAlias = useCallback(
		(memberId: number, type: "ko" | "ja", rawAlias: string) => {
			const alias = rawAlias.trim();
			if (!alias) return;

			setOptimisticMembers({
				type: "addAlias",
				memberId,
				aliasType: type,
				alias,
			});
			void mutations.addAlias.mutateAsync({ memberId, type, alias });
		},
		[mutations.addAlias, setOptimisticMembers],
	);

	const handleRemoveAlias = useCallback(
		(memberId: number, type: "ko" | "ja", alias: string) => {
			setModal({ type: "removeAlias", memberId, aliasType: type, alias });
		},
		[],
	);

	const confirmRemoveAlias = useCallback(() => {
		if (modal.type !== "removeAlias") return;
		const payload = {
			memberId: modal.memberId,
			aliasType: modal.aliasType,
			alias: modal.alias,
		};
		setModal({ type: "none" });
		startTransition(() => {
			setOptimisticMembers({
				type: "removeAlias",
				memberId: payload.memberId,
				aliasType: payload.aliasType,
				alias: payload.alias,
			});
		});
		mutations.removeAlias.mutate({
			memberId: payload.memberId,
			type: payload.aliasType,
			alias: payload.alias,
		});
	}, [modal, mutations.removeAlias, setOptimisticMembers]);

	const handleUpdateChannel = useCallback(
		(memberId: number, memberName: string, currentChannelId: string) => {
			setModal({ type: "channelEdit", memberId, memberName, currentChannelId });
		},
		[],
	);

	const confirmUpdateChannel = useCallback(
		(newChannelId: string) => {
			if (modal.type !== "channelEdit") return;
			setOptimisticMembers({
				type: "updateChannel",
				memberId: modal.memberId,
				channelId: newChannelId,
			});
			void mutations.updateChannel.mutateAsync({
				memberId: modal.memberId,
				channelId: newChannelId,
			});
		},
		[modal, mutations.updateChannel, setOptimisticMembers],
	);

	const handleEditName = useCallback(
		(memberId: number, currentName: string) => {
			setModal({ type: "nameEdit", memberId, currentName });
		},
		[],
	);

	const confirmEditName = useCallback(
		(newName: string) => {
			if (modal.type !== "nameEdit") return;
			setOptimisticMembers({
				type: "updateName",
				memberId: modal.memberId,
				name: newName,
			});
			void mutations.updateName.mutateAsync({
				memberId: modal.memberId,
				name: newName,
			});
		},
		[modal, mutations.updateName, setOptimisticMembers],
	);

	const handleToggleGraduation = useCallback(
		(memberId: number, memberName: string, currentStatus: boolean) => {
			setModal({ type: "graduation", memberId, memberName, currentStatus });
		},
		[],
	);

	const confirmToggleGraduation = useCallback(() => {
		if (modal.type !== "graduation") return;
		const payload = {
			memberId: modal.memberId,
			isGraduated: !modal.currentStatus,
		};
		setModal({ type: "none" });
		startTransition(() => {
			setOptimisticMembers({
				type: "graduation",
				memberId: payload.memberId,
				isGraduated: payload.isGraduated,
			});
		});
		mutations.setGraduation.mutate(payload);
	}, [modal, mutations.setGraduation, setOptimisticMembers]);

	const filteredMembers = useMemo(
		() => filterMembers(optimisticMembers, deferredSearchTerm, hideGraduated),
		[deferredSearchTerm, hideGraduated, optimisticMembers],
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
		mutations,
		searchTerm,
		setSearchTerm,
		hideGraduated,
		toggleHideGraduated,
		modal,
		setModal,
		isAddModalOpen,
		setIsAddModalOpen,
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
