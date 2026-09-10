import { useMutation } from "@tanstack/react-query";
import { useCallback } from "react";
import { RequestBlockedError } from "@/api/errors";
import { session } from "@/app/bootstrap";
import toast from "@/lib/toast-api";

interface Intent<T> { variables: T; authGeneration: number }
interface BusinessMutationOptions<TData, TVariables> {
	mutationFn: (variables: TVariables) => Promise<TData>;
	onSuccess?: (data: TData, variables: TVariables) => void | Promise<void>;
	onError?: (error: Error, variables: TVariables) => void | Promise<void>;
}

/** useBusinessMutation은 제출 시 인증을 고정하고 retry·offline 대기·scope queue를 허용하지 않습니다. */
export function useBusinessMutation<TData, TVariables>(options: BusinessMutationOptions<TData, TVariables>) {
	const mutation = useMutation<TData, Error, Intent<TVariables>>({
		retry: false,
		networkMode: "always",
		mutationFn: async intent => {
			session.state.assertCurrent(intent.authGeneration);
			return options.mutationFn(intent.variables);
		},
		onSuccess: async (data, intent) => {
			if (intent.authGeneration === session.state.snapshot().authGeneration) await options.onSuccess?.(data, intent.variables);
		},
		onError: async (error, intent) => {
			if (intent.authGeneration !== session.state.snapshot().authGeneration) return;
			// 전송 전 공통 거부는 cache 무효화 등의 feature 콜백이 안내를 가리지 않게 처리합니다.
			if (error instanceof RequestBlockedError) { toast.error(error.message); return; }
			await options.onError?.(error, intent.variables);
		},
	});
	const { mutate: dispatch, mutateAsync: dispatchAsync } = mutation;
	const mutate = useCallback((variables: TVariables) => {
		dispatch({ variables, authGeneration: session.state.snapshot().authGeneration });
	}, [dispatch]);
	const mutateAsync = useCallback(async (variables: TVariables): Promise<TData> => {
		const {authGeneration} = session.state.snapshot();
		const data = await dispatchAsync({ variables, authGeneration });
		session.state.assertCurrent(authGeneration);
		return data;
	}, [dispatchAsync]);
	return { ...mutation, variables: mutation.variables?.variables, mutate, mutateAsync };
}
