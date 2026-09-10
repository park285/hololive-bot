import { QueryClient } from "@tanstack/react-query";
import { CONFIG } from "@/config";

export const queryClient = new QueryClient({
	defaultOptions: {
		queries: {
			staleTime: CONFIG.query.staleTimeMs,
			gcTime: CONFIG.query.gcTimeMs,
			retry: CONFIG.query.retry,
			refetchOnWindowFocus: false,
		},
		mutations: {
			retry: false,
			networkMode: "always",
		},
	},
});
