import { useBusinessMutation } from "@/operations/useBusinessMutation";
import { useQueryClient } from "@tanstack/react-query";
import { queryKeys } from "@/queries/keys";
import { membersApi } from "@/features/members/api";
import type {
	AddAliasRequest,
	RemoveAliasRequest,
} from "@/features/members/types";

const useInvalidateMembers = () => {
	const queryClient = useQueryClient();
	return () => {
		void queryClient.invalidateQueries({ queryKey: queryKeys.members.all });
	};
};

export const useAddAliasMutation = () => {
	const invalidate = useInvalidateMembers();
	return useBusinessMutation({
		mutationFn: ({
			memberId,
			type,
			alias,
		}: {
			memberId: string;
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
	return useBusinessMutation({
		mutationFn: ({
			memberId,
			type,
			alias,
		}: {
			memberId: string;
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
	return useBusinessMutation({
		mutationFn: ({
			memberId,
			channelId,
		}: {
			memberId: string;
			channelId: string;
		}) => membersApi.updateChannel(memberId, { channelId }),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useUpdateNameMutation = () => {
	const invalidate = useInvalidateMembers();
	return useBusinessMutation({
		mutationFn: ({ memberId, name }: { memberId: string; name: string }) =>
			membersApi.updateName(memberId, name),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useSetGraduationMutation = () => {
	const invalidate = useInvalidateMembers();
	return useBusinessMutation({
		mutationFn: ({
			memberId,
			isGraduated,
		}: {
			memberId: string;
			isGraduated: boolean;
		}) => membersApi.setGraduation(memberId, { isGraduated }),
		onSuccess: invalidate,
		onError: invalidate,
	});
};

export const useAddMemberMutation = () => {
	const invalidate = useInvalidateMembers();
	return useBusinessMutation({
		mutationFn: membersApi.add,
		onSuccess: invalidate,
		onError: invalidate,
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
