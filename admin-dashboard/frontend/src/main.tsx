import "@/theme-init";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@/index.css";
import App from "@/App";

const rootElement = document.getElementById("root");
if (!rootElement) {
	throw new Error("Failed to find the root element");
}

const renderApp = () => {
	createRoot(rootElement).render(
		<StrictMode>
			<App />
		</StrictMode>,
	);
};

const enableMocking = async () => {
	if (!(import.meta.env.DEV && import.meta.env["VITE_ENABLE_MSW"] === "true")) {
		return;
	}

	const { worker } = await import("@/mocks/browser");
	await worker.start({
		onUnhandledRequest: "bypass",
		serviceWorker: {
			url: "/mockServiceWorker.js",
		},
	});
};

void enableMocking()
	.catch((error: unknown) => {
		console.error("MSW bootstrap failed", error);
	})
	.finally(() => {
		renderApp();
	});
