import { useMutation, useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/api/queryKeys";
import { membersApi } from "@/features/members/api";
import type {
	AddAliasRequest,
	Member,
	RemoveAliasRequest,
} from "@/features/members/types";
import toast from "@/lib/toast-api";

const useInvalidateMembers = () => {
	const queryClient = useQueryClient();
	return () => {
		void queryClient.invalidateQueries({ queryKey: queryKeys.members.all });
	};
};

export const useAddAliasMutation = () => {
	const invalidate = useInvalidateMembers();
	return useMutation({
		mutationFn: ({
			memberId,
			type,
			alias,
		}: {
			memberId: number;
			type: "ko" | "ja";
			alias: string;
		}) =>
			membersApi.addAlias(memberId, { type, alias } satisfies AddAliasRequest),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useRemoveAliasMutation = () => {
	const invalidate = useInvalidateMembers();
	return useMutation({
		mutationFn: ({
			memberId,
			type,
			alias,
		}: {
			memberId: number;
			type: "ko" | "ja";
			alias: string;
		}) =>
			membersApi.removeAlias(memberId, {
				type,
				alias,
			} satisfies RemoveAliasRequest),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useUpdateChannelMutation = () => {
	const invalidate = useInvalidateMembers();
	return useMutation({
		mutationFn: ({
			memberId,
			channelId,
		}: {
			memberId: number;
			channelId: string;
		}) => membersApi.updateChannel(memberId, { channelId }),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useUpdateNameMutation = () => {
	const invalidate = useInvalidateMembers();
	return useMutation({
		mutationFn: ({ memberId, name }: { memberId: number; name: string }) =>
			membersApi.updateName(memberId, name),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useSetGraduationMutation = () => {
	const invalidate = useInvalidateMembers();
	return useMutation({
		mutationFn: ({
			memberId,
			isGraduated,
		}: {
			memberId: number;
			isGraduated: boolean;
		}) => membersApi.setGraduation(memberId, { isGraduated }),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useAddMemberMutation = () => {
	const invalidate = useInvalidateMembers();
	return useMutation({
		mutationFn: membersApi.add,
		onSuccess: invalidate,
		onError: (err: Error) => {
			invalidate();
			toast.error(`멤버 추가 실패: ${err.message}`);
		},
	});
};

export const useMemberMutations = () => ({
	addAlias: useAddAliasMutation(),
	removeAlias: useRemoveAliasMutation(),
	updateChannel: useUpdateChannelMutation(),
	updateName: useUpdateNameMutation(),
	setGraduation: useSetGraduationMutation(),
	addMember: useAddMemberMutation(),
});

export type OptimisticUpdate =
	| { type: "graduation"; memberId: number; isGraduated: boolean }
	| {
			type: "addAlias";
			memberId: number;
			aliasType: "ko" | "ja";
			alias: string;
	  }
	| {
			type: "removeAlias";
			memberId: number;
			aliasType: "ko" | "ja";
			alias: string;
	  }
	| { type: "updateChannel"; memberId: number; channelId: string }
	| { type: "updateName"; memberId: number; name: string };

export const optimisticMemberReducer = (
	state: Member[],
	update: OptimisticUpdate,
): Member[] => {
	switch (update.type) {
		case "graduation":
			return state.map((m) =>
				m.id === update.memberId
					? { ...m, isGraduated: update.isGraduated }
					: m,
			);
		case "addAlias":
			return state.map((m) =>
				m.id === update.memberId
					? {
							...m,
							aliases: {
								...m.aliases,
								[update.aliasType]: [
									...m.aliases[update.aliasType],
									update.alias,
								],
							},
						}
					: m,
			);
		case "removeAlias":
			return state.map((m) =>
				m.id === update.memberId
					? {
							...m,
							aliases: {
								...m.aliases,
								[update.aliasType]: m.aliases[update.aliasType].filter(
									(a) => a !== update.alias,
								),
							},
						}
					: m,
			);
		case "updateChannel":
			return state.map((m) =>
				m.id === update.memberId ? { ...m, channelId: update.channelId } : m,
			);
		case "updateName":
			return state.map((m) =>
				m.id === update.memberId ? { ...m, name: update.name } : m,
			);
	}
};
