import { useCallback, useEffect, useRef } from "react";

const CHANNEL_NAME = "admin_session";

interface TabSyncMessage {
	type: "ACTIVITY" | "LOGOUT";
	timestamp: number;
}

export function useTabSync(
	onActivityFromOtherTab: () => void,
	onLogoutFromOtherTab?: () => void,
) {
	const channelRef = useRef<BroadcastChannel | null>(null);

	const broadcastActivity = useCallback(() => {
		if (channelRef.current) {
			const message: TabSyncMessage = {
				type: "ACTIVITY",
				timestamp: Date.now(),
			};
			channelRef.current.postMessage(message);
		}
	}, []);

	const broadcastLogout = useCallback(() => {
		if (channelRef.current) {
			const message: TabSyncMessage = {
				type: "LOGOUT",
				timestamp: Date.now(),
			};
			channelRef.current.postMessage(message);
		}
	}, []);

	useEffect(() => {
		if (typeof BroadcastChannel === "undefined") {
			console.warn("BroadcastChannel API is not supported in this browser");
			return;
		}

		channelRef.current = new BroadcastChannel(CHANNEL_NAME);

		channelRef.current.onmessage = (event: MessageEvent<TabSyncMessage>) => {
			const { type } = event.data;

			switch (type) {
				case "ACTIVITY":
					onActivityFromOtherTab();
					break;
				case "LOGOUT":
					if (onLogoutFromOtherTab) {
						onLogoutFromOtherTab();
					}
					break;
			}
		};

		return () => {
			channelRef.current?.close();
			channelRef.current = null;
		};
	}, [onActivityFromOtherTab, onLogoutFromOtherTab]);

	return { broadcastActivity, broadcastLogout };
}
