/**
 * 애플리케이션 전역 설정 상수
 * 하드코딩된 값들을 중앙화하여 유지보수성 향상
 */

/** 시스템 리소스 차트 서비스별 색상 매핑 */
const serviceColors: Record<string, string> = {
	"hololive-bot": "#0ea5e9", // sky-500
	"admin-dashboard": "#64748b", // slate-500
};

export const CONFIG = {
	/** 세션 Heartbeat 설정 */
	heartbeat: {
		/** Heartbeat 전송 간격 (밀리초) - IdleTimeout의 절반으로 설정 */
		intervalMs: 5 * 60 * 1000, // 5분
		/** 유휴 타임아웃 (밀리초) - 이 시간 동안 활동 없으면 idle로 간주 */
		idleTimeoutMs: 10 * 60 * 1000, // 10분
		/** 로그아웃 판단을 위한 최대 연속 실패 횟수 */
		maxFailures: 3,
	},

	/** WebSocket 재연결 설정 */
	websocket: {
		/** 재연결 시도 횟수 */
		reconnectAttempts: 5,
		/** 기본 재연결 간격 (밀리초) */
		reconnectIntervalMs: 3000,
		/** 최대 백오프 지연 시간 (밀리초) */
		maxBackoffMs: 30000,
		/** Ping 전송 간격 (밀리초) - 연결 유지용 */
		pingIntervalMs: 30 * 1000, // 30초
	},

	/** TanStack Query 기본 설정 */
	query: {
		/** 데이터 신선도 유지 시간 (밀리초) */
		staleTimeMs: 5 * 60 * 1000, // 5분
		/** 가비지 컬렉션 시간 (밀리초) */
		gcTimeMs: 60 * 60 * 1000, // 1시간
		/** 재시도 횟수 */
		retry: 1,
	},

	/** API 설정 */
	api: {
		/** 요청 타임아웃 (밀리초) */
		timeoutMs: 30000,
		/** API 기본 URL */
		baseUrl: "/admin/api",
	},

	/** UI 관련 설정 */
	ui: {
		serviceColors,
	},
} as const;

/** 타입 추출용 */
export type AppConfig = typeof CONFIG;
