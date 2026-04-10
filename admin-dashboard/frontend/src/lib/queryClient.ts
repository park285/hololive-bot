import { QueryClient } from "@tanstack/react-query";
import { CONFIG } from "@/config";
import toast from "@/lib/toast-api";
import { getErrorMessageFromUnknown } from "@/lib/typeUtils";

export const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			staleTime: CONFIG.query.staleTimeMs,
			gcTime: CONFIG.query.gcTimeMs,
			retry: CONFIG.query.retry,
			refetchOnWindowFocus: false,
		},
		mutations: {
			retry: 0,
			onError: (error: Error) => {
				toast.error(getErrorMessageFromUnknown(error));
			},
		},
	},
});
