/** safeHTTPURL은 외부 링크·이미지의 HTTP(S) 주소만 허용합니다(ASVS 1.2.2). */
export function safeHTTPURL(value: string | null | undefined): URL | undefined {
	if (!value) return undefined;
	try {
		const url = new URL(value);
		if ((url.protocol !== "https:" && url.protocol !== "http:") || url.username || url.password) return undefined;
		return url;
	} catch {
		return undefined;
	}
}
