/**
 * Client Hints 유틸리티
 *
 * Google의 User-Agent 축소 정책에 따라 브라우저가 기본적으로 제공하는 UA 정보는
 * 축소된 형태입니다. 진짜 기기 정보(OS 버전, 모델명 등)를 얻으려면 Client Hints API를 사용해야 합니다.
 *
 * - 기본 UA: "Android 10" (축소됨)
 * - Client Hints: "Android 16", "SM-S928N" (실제 정보)
 *
 * @see https://developer.mozilla.org/en-US/docs/Web/API/User-Agent_Client_Hints_API
 */

/**
 * Client Hints 정보를 담는 인터페이스
 */
export interface ClientHintsData {
	/** 전체 브랜드 및 버전 목록 (예: "Google Chrome";v="131", "Chromium";v="131") */
	brands: string;
	/** 모바일 기기 여부 */
	mobile: boolean;
	/** 플랫폼 (예: "Android", "Windows", "macOS") */
	platform: string;
	/** 정확한 플랫폼 버전 (예: "16.0.0") - High Entropy */
	platformVersion: string;
	/** 기기 모델명 (예: "SM-S928N") - High Entropy */
	model: string;
	/** 아키텍처 (예: "arm", "x86") - High Entropy */
	architecture: string;
	/** 비트 수 (예: "64", "32") - High Entropy */
	bitness: string;
	/** 전체 브라우저 버전 목록 - High Entropy */
	fullVersionList: string;
	/** 폴백용 기존 User-Agent */
	userAgent: string;
}

/**
 * NavigatorUAData 인터페이스 (브라우저 타입 정의)
 */
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

/**
 * 브랜드 버전 배열을 문자열로 변환
 */
function formatBrands(brands: NavigatorUABrandVersion[] | undefined): string {
	if (!brands || brands.length === 0) return "";
	return brands.map((b) => `"${b.brand}";v="${b.version}"`).join(", ");
}

/**
 * Client Hints 정보를 수집합니다.
 *
 * 브라우저가 Client Hints API를 지원하면 High Entropy 값(정확한 OS 버전, 모델명)을 요청하고,
 * 지원하지 않으면 기존 User-Agent로 폴백합니다.
 *
 * @returns Promise<ClientHintsData> - 수집된 Client Hints 정보
 */
export async function getClientHints(): Promise<ClientHintsData> {
	// 기본값 (폴백용 User-Agent)
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

	// Client Hints API 지원 여부 확인
	if (!navigator.userAgentData) {
		return fallback;
	}

	try {
		// Low Entropy 값 (동기적으로 사용 가능)
		const uaData = navigator.userAgentData;
		const lowEntropy: ClientHintsData = {
			...fallback,
			brands: formatBrands(uaData.brands),
			mobile: uaData.mobile,
			platform: uaData.platform,
		};

		// High Entropy 값 요청 (사용자 권한 필요할 수 있음)
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
		// High Entropy 요청 실패 시 Low Entropy 반환 (userAgentData는 try 블록 진입 조건으로 항상 존재)
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

/**
 * User-Agent 문자열에서 플랫폼을 추출합니다 (폴백용)
 */
function getPlatformFromUA(ua: string): string {
	if (/Android/i.test(ua)) return "Android";
	if (/iPhone|iPad|iPod/i.test(ua)) return "iOS";
	if (/Mac OS X/i.test(ua)) return "macOS";
	if (/Windows/i.test(ua)) return "Windows";
	if (/Linux/i.test(ua)) return "Linux";
	if (/CrOS/i.test(ua)) return "Chrome OS";
	return "Unknown";
}

/**
 * Client Hints를 요약된 문자열로 변환합니다.
 * 로그 표시용으로 사람이 읽기 쉬운 형태로 반환합니다.
 *
 * @param hints - ClientHintsData 객체
 * @returns 요약 문자열 (예: "Android 16 (SM-S928N)" 또는 "Windows 11 x64")
 */
export function formatClientHintsSummary(hints: ClientHintsData): string {
	const parts: string[] = [];

	// 플랫폼 + 버전
	if (hints.platform) {
		let platformStr = hints.platform;
		if (hints.platformVersion) {
			// 버전에서 주요 부분만 추출 (예: "16.0.0" → "16")
			const [majorVersion] = hints.platformVersion.split(".");
			platformStr += ` ${majorVersion ?? ""}`;
		}
		parts.push(platformStr);
	}

	// 모델명 (모바일인 경우)
	if (hints.model) {
		parts.push(`(${hints.model})`);
	} else if (hints.architecture) {
		// 데스크톱인 경우 아키텍처 표시
		const arch = hints.bitness
			? `${hints.architecture}${hints.bitness}`
			: hints.architecture;
		parts.push(arch);
	}

	// 모바일 표시
	if (hints.mobile && !hints.model) {
		parts.push("[Mobile]");
	}

	return parts.join(" ") || hints.userAgent.slice(0, 50);
}

/**
 * HTTP 요청에 포함할 Client Hints 헤더 객체를 생성합니다.
 *
 * @param hints - ClientHintsData 객체
 * @returns 헤더 객체
 */
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
