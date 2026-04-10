export interface ClientHintsData {
	brands: string;
	mobile: boolean;
	platform: string;
	platformVersion: string;
	model: string;
	architecture: string;
	bitness: string;
	fullVersionList: string;
	userAgent: string;
}

interface NavigatorUABrandVersion {
	brand: string;
	version: string;
}

interface UADataValues {
	brands?: NavigatorUABrandVersion[];
	mobile?: boolean;
	platform?: string;
	platformVersion?: string;
	model?: string;
	architecture?: string;
	bitness?: string;
	fullVersionList?: NavigatorUABrandVersion[];
}

interface NavigatorUAData {
	brands: NavigatorUABrandVersion[];
	mobile: boolean;
	platform: string;
	getHighEntropyValues(hints: string[]): Promise<UADataValues>;
}

declare global {
	interface Navigator {
		userAgentData?: NavigatorUAData;
	}
}

function formatBrands(brands: NavigatorUABrandVersion[] | undefined): string {
	if (!brands || brands.length === 0) return "";
	return brands.map((b) => `"${b.brand}";v="${b.version}"`).join(", ");
}

export async function getClientHints(): Promise<ClientHintsData> {
	const fallback: ClientHintsData = {
		brands: "",
		mobile: /Mobile|Android|iPhone|iPad/i.test(navigator.userAgent),
		platform: getPlatformFromUA(navigator.userAgent),
		platformVersion: "",
		model: "",
		architecture: "",
		bitness: "",
		fullVersionList: "",
		userAgent: navigator.userAgent,
	};

	if (!navigator.userAgentData) {
		return fallback;
	}

	try {
		const uaData = navigator.userAgentData;
		const lowEntropy: ClientHintsData = {
			...fallback,
			brands: formatBrands(uaData.brands),
			mobile: uaData.mobile,
			platform: uaData.platform,
		};

		const highEntropyValues = await uaData.getHighEntropyValues([
			"platformVersion",
			"model",
			"architecture",
			"bitness",
			"fullVersionList",
		]);

		return {
			...lowEntropy,
			platformVersion: highEntropyValues.platformVersion ?? "",
			model: highEntropyValues.model ?? "",
			architecture: highEntropyValues.architecture ?? "",
			bitness: highEntropyValues.bitness ?? "",
			fullVersionList: formatBrands(highEntropyValues.fullVersionList),
		};
	} catch (error) {
		console.warn("Failed to get high entropy client hints:", error);

		const uaData = navigator.userAgentData;
		return {
			...fallback,
			brands: formatBrands(uaData.brands),
			mobile: uaData.mobile,
			platform: uaData.platform,
		};
	}
}

function getPlatformFromUA(ua: string): string {
	if (/Android/i.test(ua)) return "Android";
	if (/iPhone|iPad|iPod/i.test(ua)) return "iOS";
	if (/Mac OS X/i.test(ua)) return "macOS";
	if (/Windows/i.test(ua)) return "Windows";
	if (/Linux/i.test(ua)) return "Linux";
	if (/CrOS/i.test(ua)) return "Chrome OS";
	return "Unknown";
}

export function formatClientHintsSummary(hints: ClientHintsData): string {
	const parts: string[] = [];

	if (hints.platform) {
		let platformStr = hints.platform;
		if (hints.platformVersion) {
			const [majorVersion] = hints.platformVersion.split(".");
			platformStr += ` ${majorVersion ?? ""}`;
		}
		parts.push(platformStr);
	}

	if (hints.model) {
		parts.push(`(${hints.model})`);
	} else if (hints.architecture) {
		const arch = hints.bitness
			? `${hints.architecture}${hints.bitness}`
			: hints.architecture;
		parts.push(arch);
	}

	if (hints.mobile && !hints.model) {
		parts.push("[Mobile]");
	}

	return parts.join(" ") || hints.userAgent.slice(0, 50);
}

export function getClientHintsHeaders(
	hints: ClientHintsData,
): Record<string, string> {
	const headers: Record<string, string> = {};

	if (hints.brands) {
		headers["Sec-CH-UA"] = hints.brands;
	}
	if (hints.fullVersionList) {
		headers["Sec-CH-UA-Full-Version-List"] = hints.fullVersionList;
	}
	headers["Sec-CH-UA-Mobile"] = hints.mobile ? "?1" : "?0";
	if (hints.platform) {
		headers["Sec-CH-UA-Platform"] = `"${hints.platform}"`;
	}
	if (hints.platformVersion) {
		headers["Sec-CH-UA-Platform-Version"] = `"${hints.platformVersion}"`;
	}
	if (hints.model) {
		headers["Sec-CH-UA-Model"] = `"${hints.model}"`;
	}
	if (hints.architecture) {
		headers["Sec-CH-UA-Arch"] = `"${hints.architecture}"`;
	}
	if (hints.bitness) {
		headers["Sec-CH-UA-Bitness"] = `"${hints.bitness}"`;
	}

	return headers;
}
