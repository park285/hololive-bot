import { useState, type SyntheticEvent } from 'react'
import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { streamsApi } from '@/api/holo'
import { queryKeys } from '@/api/queryKeys'
import ExternalLink from 'lucide-react/dist/esm/icons/external-link';
import Calendar from 'lucide-react/dist/esm/icons/calendar';
import PlayCircle from 'lucide-react/dist/esm/icons/play-circle';
import ChevronDown from 'lucide-react/dist/esm/icons/chevron-down';
import type { Stream, StreamOrg } from '@/types'

/**
 * YouTube 썸네일 품질 옵션
 * - 'max': maxresdefault (1280x720) - Live 스트림 카드용
 * - 'sd': sddefault (640x480) - Upcoming 리스트용
 * - 'high': hqdefault (480x360) - 작은 썸네일용
 */
type ThumbnailQuality = 'max' | 'sd' | 'high'

interface ThumbnailSource {
    src: string
    srcSet?: string
    sizes?: string
    fallbackChain: string[]
}

const extractYouTubeVideoId = (url?: string): string | undefined => {
    if (!url) return undefined

    const youtubePatterns = [
        /\/vi\/([^/]+)\//,
        /\/vi_webp\/([^/]+)\//,
        /[?&]v=([^&]+)/,
        /youtu\.be\/([^?&/]+)/,
    ]

    for (const pattern of youtubePatterns) {
        const match = url.match(pattern)
        if (match?.[1]) {
            return match[1]
        }
    }

    return undefined
}

const getThumbnailSource = (url?: string, quality: ThumbnailQuality = 'high'): ThumbnailSource | undefined => {
    if (!url) return undefined

    const videoId = extractYouTubeVideoId(url)
    if (!videoId) {
        return {
            src: url,
            fallbackChain: [url],
        }
    }

    const directUrls = {
        max: `https://i.ytimg.com/vi/${videoId}/maxresdefault.jpg`,
        sd: `https://i.ytimg.com/vi/${videoId}/sddefault.jpg`,
        high: `https://i.ytimg.com/vi/${videoId}/hqdefault.jpg`,
    }

    if (quality === 'max') {
        return {
            src: directUrls.max,
            srcSet: `${directUrls.high} 480w, ${directUrls.sd} 640w, ${directUrls.max} 1280w`,
            sizes: '(min-width: 1024px) 33vw, (min-width: 768px) 50vw, 100vw',
            fallbackChain: [directUrls.sd, directUrls.high, url],
        }
    }

    if (quality === 'sd') {
        return {
            src: directUrls.sd,
            srcSet: `${directUrls.high} 480w, ${directUrls.sd} 640w`,
            sizes: '(min-width: 1024px) 40vw, 100vw',
            fallbackChain: [directUrls.high, url],
        }
    }

    return {
        src: directUrls.high,
        fallbackChain: [url],
    }
}

const StreamsTab = () => {
    const [selectedOrg, setSelectedOrg] = useState<StreamOrg>('hololive')

    const orgOptions: Array<{ value: StreamOrg; label: string }> = [
        { value: 'hololive', label: 'Hololive' },
        { value: 'vspo', label: 'VSpo' },
        { value: 'stellive', label: 'Stellive' },
        { value: 'indie', label: 'Indie' },
        { value: 'all', label: 'All' },
    ]

    const { data: liveData, isLoading: liveLoading } = useQuery({
        queryKey: queryKeys.streams.live(selectedOrg),
        queryFn: () => streamsApi.getLive(selectedOrg),
        refetchInterval: 60 * 1000, // 1 minute
        staleTime: 1000 * 45, // 45 seconds
        placeholderData: keepPreviousData,
    })

    const { data: upcomingData, isLoading: upcomingLoading } = useQuery({
        queryKey: queryKeys.streams.upcoming(selectedOrg),
        queryFn: () => streamsApi.getUpcoming(selectedOrg),
        refetchInterval: 60 * 1000 * 5, // 5 minutes
        staleTime: 1000 * 60 * 4, // 4 minutes
        placeholderData: keepPreviousData,
    })

    const liveStreams = liveData?.streams ?? []
    const upcomingStreams = upcomingData?.streams ?? []

    const handleThumbnailError = (event: SyntheticEvent<HTMLImageElement>) => {
        const element = event.currentTarget
        const fallbackChain = element.dataset['fallbackChain']?.split('|').filter(Boolean) ?? []
        const nextFallback = fallbackChain.shift()

        if (nextFallback) {
            element.dataset['fallbackChain'] = fallbackChain.join('|')
            element.src = nextFallback
            return
        }

        element.style.display = 'none'
    }

    return (
        <div className="space-y-6">
            <div className="bg-white rounded-xl shadow-sm border border-slate-200 p-6">
                <div className="flex items-center justify-between gap-3 mb-4">
                    <div className="flex items-center gap-2">
                        <PlayCircle className="text-rose-500" />
                        <h3 className="text-lg font-bold text-slate-800">Live Streams</h3>
                        <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-rose-100 text-rose-600">
                            {liveStreams.length}
                        </span>
                    </div>

                    <div className="relative">
                        <select
                            value={selectedOrg}
                            onChange={(e) => { setSelectedOrg(e.target.value as StreamOrg); }}
                            className="appearance-none bg-slate-50 border border-slate-200 text-slate-700 text-sm font-medium rounded-lg py-2 pl-3 pr-8 focus:outline-none focus:ring-2 focus:ring-sky-500 focus:border-transparent cursor-pointer hover:bg-slate-100 transition-colors"
                            aria-label="스트림 org 선택"
                        >
                            {orgOptions.map((option) => (
                                <option key={option.value} value={option.value}>
                                    {option.label}
                                </option>
                            ))}
                        </select>
                        <ChevronDown className="absolute right-2.5 top-2.5 text-slate-400 pointer-events-none" size={16} />
                    </div>
                </div>

                {liveLoading ? (
                    <div className="h-40 flex items-center justify-center text-slate-400 text-sm">Loading…</div>
                ) : (
                        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {liveStreams.map((stream: Stream, index: number) => (
                                (() => {
                                    const thumbnail = getThumbnailSource(stream.thumbnail, 'max')

                                    return (
                                        <a
                                            key={stream.id}
                                            href={stream.link || `https://www.youtube.com/watch?v=${stream.id}`}
                                            target="_blank"
                                            rel="noreferrer"
                                            className="group relative block rounded-xl overflow-hidden border border-slate-200 hover:shadow-md transition-shadow [content-visibility:auto] contain-intrinsic-size-[300px]"
                                        >
                                            {thumbnail ? (
                                                <div className="aspect-video relative overflow-hidden bg-slate-100">
                                                    <img
                                                        src={thumbnail.src}
                                                        srcSet={thumbnail.srcSet}
                                                        sizes={thumbnail.sizes}
                                                        data-fallback-chain={thumbnail.fallbackChain.join('|')}
                                                        alt={stream.title}
                                                        loading={index === 0 ? "eager" : "lazy"}
                                                        decoding="async"
                                                        fetchPriority={index === 0 ? "high" : "auto"}
                                                        className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500"
                                                        onError={handleThumbnailError}
                                                    />
                                                    <div className="absolute top-2 right-2 bg-rose-600 text-white text-[10px] font-bold px-1.5 py-0.5 rounded flex items-center gap-1 shadow-sm">
                                                        LIVE
                                                    </div>
                                                </div>
                                            ) : (
                                                <div className="aspect-video bg-slate-100 flex items-center justify-center text-slate-300">
                                                    <PlayCircle size={32} />
                                                </div>
                                            )}
                                            <div className="p-4">
                                                <h4 className="font-bold text-sm line-clamp-2 mb-1 text-slate-800">{stream.title}</h4>
                                                <p className="text-xs text-slate-500 mb-3">{stream.channel_name}</p>
                                                <span className="inline-flex items-center text-xs font-medium text-red-600 group-hover:text-red-700 group-hover:underline">
                                                    <ExternalLink size={12} className="mr-1" /> Watch on YouTube
                                                </span>
                                            </div>
                                        </a>
                                    )
                                })()
                        ))}
                        {liveStreams.length === 0 && (
                            <p className="col-span-full text-center text-slate-400 text-sm py-10">No live streams currently.</p>
                        )}
                    </div>
                )}
            </div>

            <div className="bg-white rounded-xl shadow-sm border border-slate-200 p-6">
                <div className="flex items-center gap-2 mb-4">
                    <Calendar className="text-sky-500" />
                    <h3 className="text-lg font-bold text-slate-800">Upcoming Streams (24h)</h3>
                    <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-sky-100 text-sky-600">
                        {upcomingStreams.length}
                    </span>
                </div>

                {upcomingLoading ? (
                    <div className="h-40 flex items-center justify-center text-slate-400 text-sm">Loading…</div>
                ) : (
                    <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                        {upcomingStreams.map((stream: Stream) => (
                            (() => {
                                const thumbnail = getThumbnailSource(stream.thumbnail, 'sd')

                                return (
                                    <a
                                        key={stream.id}
                                        href={stream.link || `https://www.youtube.com/watch?v=${stream.id}`}
                                        target="_blank"
                                        rel="noreferrer"
                                        className="flex items-center p-3 rounded-lg border border-slate-100 hover:bg-slate-50 transition-colors group [content-visibility:auto] contain-intrinsic-size-[80px]"
                                    >
                                        <div className="w-20 h-14 rounded-lg overflow-hidden shrink-0 bg-slate-100 mr-4 relative flex items-center justify-center text-slate-300">
                                            {thumbnail ? (
                                                <img
                                                    src={thumbnail.src}
                                                    srcSet={thumbnail.srcSet}
                                                    sizes={thumbnail.sizes}
                                                    data-fallback-chain={thumbnail.fallbackChain.join('|')}
                                                    alt={stream.title}
                                                    loading="lazy"
                                                    decoding="async"
                                                    className="w-full h-full object-cover"
                                                    onError={handleThumbnailError}
                                                />
                                            ) : (
                                                <PlayCircle size={20} />
                                            )}
                                        </div>
                                        <div className="flex-1 min-w-0">
                                            <h4 className="font-medium text-sm text-slate-900 truncate group-hover:text-sky-600 transition-colors">{stream.title}</h4>
                                            <p className="text-xs text-slate-500 mt-0.5">{stream.channel_name}</p>
                                        </div>
                                        <div className="ml-4 text-right shrink-0 flex flex-col items-end gap-1">
                                            <div className="text-xs font-bold text-slate-700 bg-slate-100 px-2 py-1 rounded whitespace-nowrap">
                                                {stream.start_scheduled ? new Date(stream.start_scheduled).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : 'TBA'}
                                            </div>
                                            <span
                                                className="inline-flex items-center gap-1 text-[10px] text-red-600 hover:text-red-700 hover:bg-red-50 px-2 py-0.5 rounded transition-colors"
                                            >
                                                YouTube
                                                <ExternalLink size={10} />
                                            </span>
                                        </div>
                                    </a>
                                )
                            })()
                        ))}
                        {upcomingStreams.length === 0 && (
                            <p className="col-span-full text-center text-slate-400 text-sm py-10">No upcoming streams found.</p>
                        )}
                    </div>
                )}
            </div>
        </div>
    )
}

export default StreamsTab
