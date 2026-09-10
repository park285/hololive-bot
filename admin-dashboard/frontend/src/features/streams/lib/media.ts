import { safeHTTPURL } from "@/lib/urls";
import type { Stream } from "@/features/streams/types";

export type ThumbnailQuality = "max" | "sd" | "high";

export interface ThumbnailSource {
	src: string;
	srcSet?: string;
	sizes?: string;
	fallbackChain: string[];
}

export const extractYouTubeVideoId = (url?: string): string | undefined => {
	if (!url) return undefined;

	const youtubePatterns = [
		/\/vi\/([^/]+)\//,
		/\/vi_webp\/([^/]+)\//,
		/[?&]v=([^&]+)/,
		/youtu\.be\/([^?&/]+)/,
	];

	for (const pattern of youtubePatterns) {
		const match = url.match(pattern);
		if (match?.[1]) {
			return match[1];
		}
	}

	return undefined;
};

const resolveChzzkThumbnailTemplate = (
	url: string,
	quality: ThumbnailQuality,
): ThumbnailSource => {
	const qualityByType: Record<ThumbnailQuality, string[]> = {
		max: ["720", "480", "360", "270", "144"],
		sd: ["480", "360", "270", "144"],
		high: ["360", "270", "144"],
	};

	const [primary = "360", ...fallbacks] = qualityByType[quality];

	return {
		src: url.replace("{type}", primary),
		fallbackChain: fallbacks.map((type) => url.replace("{type}", type)),
	};
};

export const getThumbnailSource = (
	url?: string,
	quality: ThumbnailQuality = "high",
): ThumbnailSource | undefined => {
	if (!safeHTTPURL(url)) return undefined;
	if (!url) return undefined;

	if (url.includes("{type}")) {
		return resolveChzzkThumbnailTemplate(url, quality);
	}

	const videoId = extractYouTubeVideoId(url);
	if (!videoId) {
		return {
			src: url,
			fallbackChain: [],
		};
	}

	const directUrls = {
		max: `https://i.ytimg.com/vi/${encodeURIComponent(videoId)}/maxresdefault.jpg`,
		sd: `https://i.ytimg.com/vi/${encodeURIComponent(videoId)}/sddefault.jpg`,
		high: `https://i.ytimg.com/vi/${encodeURIComponent(videoId)}/hqdefault.jpg`,
	};

	if (quality === "max") {
		return {
			src: directUrls.max,
			srcSet: `${directUrls.high} 480w, ${directUrls.sd} 640w, ${directUrls.max} 1280w`,
			sizes: "(min-width: 1024px) 33vw, (min-width: 768px) 50vw, 100vw",
			fallbackChain: [directUrls.sd, directUrls.high, url],
		};
	}

	if (quality === "sd") {
		return {
			src: directUrls.sd,
			srcSet: `${directUrls.high} 480w, ${directUrls.sd} 640w`,
			sizes: "(min-width: 1024px) 40vw, 100vw",
			fallbackChain: [directUrls.high, url],
		};
	}

	return {
		src: directUrls.high,
		fallbackChain: [url],
	};
};

/** getStreamLinkMeta는 URL hostname으로 플랫폼을 구분하고 안전하지 않은 링크를 활성화하지 않습니다. */
export const getStreamLinkMeta = (stream: Pick<Stream, "id" | "link">) => {
	const url = safeHTTPURL(stream.link || (stream.id ? `https://www.youtube.com/watch?v=${encodeURIComponent(stream.id)}` : undefined));
	if (url === undefined) return { href: undefined, label: "사용할 수 없는 방송 링크", badge: "확인 필요" };
	if (url.hostname === "chzzk.naver.com") return { href: url.href, label: "Watch on CHZZK", badge: "CHZZK" };
	if (url.hostname === "youtube.com" || url.hostname.endsWith(".youtube.com") || url.hostname === "youtu.be") return { href: url.href, label: "Watch on YouTube", badge: "YouTube" };
	return { href: url.href, label: "Open link", badge: "Link" };
};

export const getStreamKey = (
	stream: Pick<Stream, "id" | "channel_id" | "title" | "link">,
	index: number,
) =>
	stream.id ||
	stream.link ||
	`${stream.channel_id}:${stream.title}:${String(index)}`;
