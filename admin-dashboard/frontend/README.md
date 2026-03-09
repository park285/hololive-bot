# Hololive Kakao Bot Admin UI

홀로라이브 카카오 봇 관리자 대시보드입니다.  
React 19 + TypeScript + Vite 7 기반으로 구축되었으며, 실시간 모니터링과 봇 설정 관리 기능을 제공합니다.

---

## 📋 목차

- [기술 스택](#-기술-스택)
- [프로젝트 구조](#-프로젝트-구조)
- [주요 기능](#-주요-기능)
- [설치 및 실행](#-설치-및-실행)
- [아키텍처](#-아키텍처)
- [API 통합](#-api-통합)
- [컴포넌트 상세](#-컴포넌트-상세)
- [개발 가이드](#-개발-가이드)
- [TypeScript 설정](#-typescript-설정)
- [ESLint 설정](#-eslint-설정)
- [배포](#-배포)

---

## 🛠 기술 스택

### 코어
| 패키지 | 버전 | 용도 |
|--------|------|------|
| React | ^19.2.3 | UI 라이브러리 (React 19) |
| TypeScript | ~5.9.3 | 타입 안전성 |
| Vite | ^7.3.0 | 빌드 도구 |

### 상태 관리 및 데이터 페칭
| 패키지 | 버전 | 용도 |
|--------|------|------|
| @tanstack/react-query | ^5.90.14 | 서버 상태 관리 |
| Zustand | ^5.0.9 | 클라이언트 상태 관리 (persist middleware 사용) |
| Axios | ^1.13.2 | HTTP 클라이언트 |

### UI 및 스타일링
| 패키지 | 버전 | 용도 |
|--------|------|------|
| TailwindCSS | ^4.1.18 | 유틸리티 CSS (v4 + @tailwindcss/vite 플러그인) |
| tailwindcss-animate | ^1.0.7 | 애니메이션 플러그인 |
| shadcn/ui | - | Radix 기반 UI 컴포넌트 |
| Framer Motion | ^12.23.26 | 애니메이션 |
| Lucide React | ^0.561.0 | 아이콘 |
| clsx + tailwind-merge | - | 조건부 클래스 병합 (`cn` 유틸리티) |
| @headlessui/react | ^2.2.9 | 접근성 준수 UI 컴포넌트 |

### 폼 및 유효성 검사
| 패키지 | 버전 | 용도 |
|--------|------|------|
| react-hook-form | ^7.69.0 | 폼 관리 |
| Zod | ^4.2.1 | 스키마 유효성 검사 |
| @hookform/resolvers | ^5.2.2 | Zod ↔ react-hook-form 통합 |

### 시각화 및 가상화
| 패키지 | 버전 | 용도 |
|--------|------|------|
| Recharts | ^3.6.0 | 차트 라이브러리 (AreaChart) |
| @tanstack/react-virtual | ^3.13.13 | 가상화 스크롤 (5000줄 로그 렌더링) |

### 라우팅
| 패키지 | 버전 | 용도 |
|--------|------|------|
| react-router-dom | ^7.11.0 | SPA 라우팅 (createBrowserRouter) |

### 알림
| 패키지 | 버전 | 용도 |
|--------|------|------|
| react-hot-toast | ^2.6.0 | 토스트 알림 |

### 빌드 최적화
| 패키지 | 버전 | 용도 |
|--------|------|------|
| babel-plugin-react-compiler | ^1.0.0 | React Compiler (자동 메모이제이션) |

---

## 📁 프로젝트 구조

```
admin-ui/
├── src/
│   ├── api/                        # API 클라이언트
│   │   ├── client.ts               # Axios 인스턴스 (인터셉터, 타임아웃, 401/429 처리)
│   │   └── index.ts                # API 함수 모음 (10개 모듈)
│   │
│   ├── components/                 # UI 컴포넌트
│   │   ├── dashboard/              # 대시보드 전용
│   │   │   └── SystemStatsChart.tsx    # 실시간 시스템 자원 차트 (Recharts + WebSocket)
│   │   │
│   │   ├── docker/                 # Docker 관련
│   │   │   ├── DockerContainerItem.tsx # 컨테이너 카드 (시작/중지/재시작)
│   │   │   └── LogTerminal.tsx         # 실시간 로그 터미널 (가상화 + ANSI 스트리핑)
│   │   │
│   │   ├── ui/                     # 재사용 가능한 UI (shadcn/ui 기반)
│   │   │   ├── Badge.tsx           # 상태 배지
│   │   │   ├── Button.tsx          # 버튼 (variant, size)
│   │   │   ├── Card.tsx            # 카드 (Header, Body, Footer)
│   │   │   ├── Form.tsx            # 폼 컨트롤 (react-hook-form 연동)
│   │   │   ├── Input.tsx           # 입력 필드
│   │   │   ├── Label.tsx           # 레이블
│   │   │   ├── StatCard.tsx        # 통계 카드 (아이콘, 값, 클릭)
│   │   │   ├── TabButton.tsx       # 탭 버튼
│   │   │   └── index.ts            # 배럴 export
│   │   │
│   │   ├── StatsTab.tsx            # 대시보드 개요 + 채널 통계 테이블
│   │   ├── MembersTab.tsx          # 멤버 관리 (Optimistic UI, useOptimistic)
│   │   ├── AlarmsTab.tsx           # 알람 관리 (그룹화, 이름 편집)
│   │   ├── RoomsTab.tsx            # 방 ACL 관리 (토글, 화이트리스트)
│   │   ├── StreamsTab.tsx          # 라이브/예정 스트림 (wsrv.nl 이미지 최적화)
│   │   ├── LogsTab.tsx             # 시스템 로그 + Docker 실시간 로그
│   │   ├── SettingsTab.tsx         # 설정 (react-hook-form + Zod) + Docker 컨테이너 관리
│   │   │
│   │   ├── AddMemberModal.tsx      # 멤버 추가 모달
│   │   ├── ChannelEditModal.tsx    # 채널 ID 수정 모달
│   │   ├── ConfirmModal.tsx        # 확인 모달 (삭제, 상태 변경)
│   │   ├── EditNameModal.tsx       # 이름 편집 모달
│   │   ├── ErrorPage.tsx           # 에러 경계 UI (React Router errorElement)
│   │   └── MemberCard.tsx          # 멤버 카드 (별칭, 채널, 졸업 상태)
│   │
│   ├── hooks/                      # 커스텀 Hooks
│   │   └── useWebSocket.ts         # WebSocket 연결 관리
│   │                               # - Latest Ref Pattern (콜백 안정화)
│   │                               # - Exponential Backoff (최대 30초)
│   │                               # - 마운트/언마운트 안전 처리
│   │
│   ├── layouts/                    # 레이아웃
│   │   └── AppLayout.tsx           # 메인 레이아웃 (사이드바, 헤더, Outlet)
│   │                               # - 사이드바 접기/펼치기
│   │                               # - Glassmorphism 헤더
│   │
│   ├── lib/                        # 유틸리티 라이브러리
│   │   ├── utils.ts                # cn() 함수 (clsx + tailwind-merge)
│   │   └── typeUtils.ts            # 타입 안전성 유틸리티
│   │                               # - extractErrorMessage()
│   │                               # - extractStringProperty()
│   │                               # - hasProperty() 타입 가드
│   │                               # - getErrorMessageFromUnknown()
│   │
│   ├── pages/                      # 페이지
│   │   └── LoginPage.tsx           # 로그인 페이지 (Framer Motion 애니메이션)
│   │
│   ├── stores/                     # Zustand 상태 저장소
│   │   └── authStore.ts            # 인증 상태 (persist → localStorage 'admin-auth')
│   │
│   ├── types/                      # TypeScript 타입 정의
│   │   └── index.ts                # 공유 타입 (Member, Alarm, Stream, Settings 등)
│   │
│   ├── utils/                      # 유틸리티 함수
│   │   └── ssr.ts                  # SSR 데이터 소비 유틸리티
│   │                               # - getSSRData(), getSSRDataFor()
│   │                               # - consumeSSRData() (일회성 소비)
│   │                               # - hasSSRData()
│   │
│   ├── App.tsx                     # 앱 진입점
│   │                               # - QueryClient 설정 (staleTime 5분, gcTime 1시간)
│   │                               # - ProtectedRoute (Heartbeat 보안 강화: idle 감지, 절대 만료, 토큰 갱신)
│   │                               # - Lazy Loading (코드 스플리팅)
│   │                               # - createBrowserRouter
│   │
│   ├── main.tsx                    # React DOM 렌더링 (StrictMode)
│   └── index.css                   # 글로벌 스타일
│                                   # - TailwindCSS v4 @theme 설정
│                                   # - CSS 변수 기반 테마 (HSL)
│                                   # - Glassmorphism 유틸리티 (.glass, .glass-dark)
│                                   # - 커스텀 스크롤바
│
├── public/
│   └── favicon.svg
│
├── index.html                      # HTML 엔트리
│                                   # - Google Fonts (Inter) preconnect
│                                   # - SEO 메타 태그
│
├── vite.config.ts                  # Vite 설정
│                                   # - @tailwindcss/vite 플러그인
│                                   # - babel-plugin-react-compiler (target: '19')
│                                   # - 경로 별칭 (@/ → src/)
│                                   # - manualChunks (vendor 분리)
│                                   # - 개발 프록시 (/admin/api → localhost:30001)
│
├── tsconfig.app.json               # TypeScript 설정 (엄격 모드)
├── eslint.config.js                # ESLint 설정 (Type-aware, ANY 금지)
├── components.json                 # shadcn/ui 설정
└── package.json
```

---

## 🚀 주요 기능

### 1. 대시보드 (`/dashboard/stats`)
- **시스템 통계**: 멤버 수, 알람 수, 허용된 방 수, 버전, 업타임
- **실시간 시스템 모니터링**: WebSocket으로 CPU, 메모리, Goroutine 수 스트리밍
- **채널 통계 테이블**: 구독자, 영상 수, 총 조회수
- **빠른 액션**: 각 탭으로의 바로가기 버튼

### 2. 방송 현황 (`/dashboard/streams`)
- **라이브 스트림**: 현재 진행 중인 방송 목록
- **예정된 스트림**: 예정된 방송 일정
- **썸네일 최적화**: wsrv.nl 프록시를 통한 이미지 최적화 (WebP 변환, 리사이징)
- **자동 새로고침**: keepPreviousData로 깜빡임 방지

### 3. 멤버 관리 (`/dashboard/members`)
- **멤버 목록**: 검색, 졸업 멤버 필터링
- **별칭 관리**: 한국어/일본어 별칭 추가/삭제
- **채널 ID 수정**: YouTube 채널 연결
- **이름 수정**: 멤버 표시 이름 변경
- **졸업 상태 토글**: 활성/비활성화
- **Optimistic UI**: `useOptimistic` 훅으로 즉각적인 UI 반응
- **SSR 데이터 프리페칭**: `consumeSSRData('members')` 활용

### 4. 알람 관리 (`/dashboard/alarms`)
- **알람 그룹핑**: 방/유저별 접기/펼치기
- **알람 삭제**: 개별 알람 해제
- **이름 편집**: 방 이름, 유저 이름 커스텀 설정

### 5. 방 관리 (`/dashboard/rooms`)
- **ACL 토글**: 방 접근 제어 활성화/비활성화
- **화이트리스트**: 허용된 방 목록 관리
- **방 추가/삭제**: 채팅방 ID 기반

### 6. 로그 (`/dashboard/logs`)
- **시스템 로그**: 봇 이벤트 로그 (타입별 아이콘: 보안, 활동 등)
- **Docker 실시간 로그**: 컨테이너 선택 → WebSocket 스트리밍
- **로그 터미널 기능**:
  - ANSI 이스케이프 코드 제거 (`stripAnsi`)
  - 로그 레벨별 색상 하이라이팅 (INF/WRN/ERR/DBG/TRC/FTL)
  - 가상화 스크롤 (5000줄 버퍼, `@tanstack/react-virtual`)
  - 자동 스크롤 (최신 로그)
  - 연결 상태 표시 (Live/Connecting/Disconnected)

### 7. 설정 (`/dashboard/settings`)
- **알람 설정**:
  - 사전 알림 시간 (분 단위, 1~60분)
  - react-hook-form + Zod 유효성 검사
  - 토스트 알림 (react-hot-toast)
  - Dirty 상태 추적
- **Docker 컨테이너 관리**:
  - 컨테이너 목록 (이름, 상태, 헬스 체크)
  - 시작/중지/재시작 (확인 모달)
  - 실시간 상태 표시 (running/exited/healthy/unhealthy)
- **SSR 데이터 프리페칭**: `consumeSSRData('settings')` 활용

---

## 💻 설치 및 실행

### 사전 요구사항
- Node.js 20+
- npm 10+

### 개발 환경 설정

```bash
# 의존성 설치
npm install

# 개발 서버 실행 (포트 5173)
npm run dev

# 브라우저에서 http://localhost:5173 접속
# /admin/api/* 요청은 localhost:30001로 프록시됨
```

### 프로덕션 빌드

```bash
# TypeScript 컴파일 및 Vite 빌드
npm run build

# 빌드 결과물 미리보기
npm run preview
```

### 린팅

```bash
npm run lint
```

---

## 🏗 아키텍처

### 라우팅 구조

```
/login              → LoginPage (공개)
/                   → /dashboard로 리다이렉트
/dashboard          → AppLayout (보호됨, ProtectedRoute)
  ├── /             → /dashboard/stats로 리다이렉트
  ├── /stats        → StatsTab (Lazy)
  ├── /streams      → StreamsTab (Lazy)
  ├── /members      → MembersTab (Lazy)
  ├── /alarms       → AlarmsTab (Lazy)
  ├── /rooms        → RoomsTab (Lazy)
  ├── /logs         → LogsTab (Lazy)
  └── /settings     → SettingsTab (Lazy)
/*                  → /dashboard로 리다이렉트
```

### 인증 흐름

```
1. 로그인 요청
   POST /admin/api/auth/login { username, password }
   → 세션 쿠키 설정 (withCredentials: true)
   → Zustand authStore.setAuthenticated(true)
   → localStorage 'admin-auth'에 persist

2. 세션 유지 (Heartbeat) - 보안 강화
   - 5분 간격으로 POST /admin/api/auth/heartbeat { idle: boolean }
   - Pre-warning 전략: 9분 유휴 시 클라이언트 경고 → 10분 시 idle=true 전송
   - idle=false: 세션 TTL 갱신 + 토큰 갱신 (새 세션 ID 발급, Grace Period 30초)
   - idle=true: 세션 TTL 10초로 단축 (로그아웃 확정)
   - 3회 연속 실패 시 자동 로그아웃
   - 절대 만료 시간 (8시간) 초과 시 무조건 재로그인 강제

3. 보안 메커니즘 (OWASP 준수)
   - 활동 감지 기반 하트비트: 10분 유휴 시 세션 TTL 10초로 단축
   - 절대 만료 시간 (Absolute Timeout): 8시간 후 무조건 재인증
   - 토큰 갱신 (Token Rotation): 하트비트 시 새 세션 ID 발급
   - Race Condition 방지: 기존 세션 Grace Period 30초 유지
   → 상세 문서: docs/api/session_security.md

4. 401 응답 처리 (인터셉터)
   - absolute_expired=true: 절대 만료 → 즉시 로그아웃
   - authStore.logout() 호출
   - /login으로 리다이렉트

5. 429 응답 처리 (Rate Limit)
   - 콘솔에 Retry-After 로깅
   - 로그인 페이지에서 안내 메시지 표시
```



### 데이터 페칭 전략

#### TanStack Query 설정
```typescript
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 60 * 5,    // 5분 동안 fresh
      gcTime: 1000 * 60 * 60,      // 1시간 동안 캐시 유지
      retry: 1,                    // 1회 재시도
      refetchOnWindowFocus: false, // 포커스 시 리페치 비활성화
    },
    mutations: {
      retry: 0,                    // 뮤테이션 재시도 없음
    },
  },
})
```

#### SSR 데이터 프리페칭

Go 백엔드가 경로별로 `window.__SSR_DATA__`를 주입합니다:
- `/dashboard/members` → `{ members: {...} }`
- `/dashboard/settings` → `{ settings: {...}, docker: {...}, containers: {...} }`

```typescript
// 프론트엔드에서 소비
const ssrMembers = consumeSSRData('members')
const { data } = useQuery({
  queryKey: ['members'],
  queryFn: membersApi.getAll,
  initialData: ssrMembers, // 초기 로드 시 페칭 생략
})
```

### 실시간 통신 (WebSocket)

| 엔드포인트 | 용도 | 데이터 |
|-----------|------|--------|
| `/admin/api/holo/ws/system-stats` | 시스템 리소스 | `{ cpuUsage, memoryUsage, memoryTotal, memoryUsed, goroutines }` |
| `/admin/api/docker/containers/{name}/logs/stream` | Docker 로그 | 로그 라인 (문자열) |

### 코드 스플리팅

```typescript
// Eager Load (핵심 경로 - 번들에 포함)
import LoginPage from '@/pages/LoginPage'
import { AppLayout } from '@/layouts/AppLayout'
import ErrorPage from '@/components/ErrorPage'

// Lazy Load (비핵심 경로 - 별도 청크)
const StatsTab = lazy(() => import('@/components/StatsTab'))
const MembersTab = lazy(() => import('@/components/MembersTab'))
const AlarmsTab = lazy(() => import('@/components/AlarmsTab'))
const RoomsTab = lazy(() => import('@/components/RoomsTab'))
const StreamsTab = lazy(() => import('@/components/StreamsTab'))
const LogsTab = lazy(() => import('@/components/LogsTab'))
const SettingsTab = lazy(() => import('@/components/SettingsTab'))
```

### 번들 최적화 (Manual Chunks)

```typescript
// vite.config.ts
manualChunks: {
  'vendor-react': ['react', 'react-dom'],
  'vendor-router': ['react-router-dom'],
  'vendor-motion': ['framer-motion'],
  'vendor-query': ['@tanstack/react-query'],
  'vendor-icons': ['lucide-react'],
}
```

---

## 🔌 API 통합

### API 클라이언트 설정

```typescript
// src/api/client.ts
const apiClient = axios.create({
  baseURL: '/admin/api',
  withCredentials: true,          // 세션 쿠키 포함
  headers: { 'Content-Type': 'application/json' },
  timeout: 30000,                 // 30초 타임아웃
})

// Request 인터셉터: 민감 정보 URL 파라미터 제거
// Response 인터셉터: 401 → 로그아웃, 429 → Rate limit 로깅
```

### 개발 시 프록시

```typescript
// vite.config.ts
server: {
  port: 5173,
  proxy: {
    '/admin/api': {
      target: 'http://localhost:30001',
      changeOrigin: true,
    },
  },
}
```

### API 모듈 목록

| 모듈 | 설명 | 주요 메서드 |
|------|------|------------|
| `authApi` | 인증 | `login(u, p)`, `logout()`, `heartbeat(idle?) → { status, rotated?, absolute_expires_at?, idle_rejected? }` |
| `membersApi` | 멤버 관리 | `getAll()`, `add(member)`, `addAlias(id, req)`, `removeAlias(id, req)`, `setGraduation(id, req)`, `updateChannel(id, req)`, `updateName(id, name)` |
| `alarmsApi` | 알람 관리 | `getAll()`, `delete(req)` |
| `roomsApi` | 방 관리 | `getAll()`, `add(req)`, `remove(req)`, `setACL(enabled)` |
| `statsApi` | 통계 | `get()`, `getChannels()` |
| `streamsApi` | 스트림 | `getLive()`, `getUpcoming()` |
| `logsApi` | 로그 | `get()` |
| `settingsApi` | 설정 | `get()`, `update(settings)` |
| `namesApi` | 이름 관리 | `setRoomName(id, name)`, `setUserName(id, name)` |
| `dockerApi` | Docker | `checkHealth()`, `getContainers()`, `restartContainer(name)`, `stopContainer(name)`, `startContainer(name)` |

---

## 📦 컴포넌트 상세

### useWebSocket Hook

```typescript
import { useWebSocket } from '@/hooks/useWebSocket'

const {
  isConnected,           // 연결 상태
  isConnecting,          // 연결 시도 중
  error,                 // 에러 이벤트
  lastMessage,           // 마지막 메시지
  connect,               // 수동 연결
  disconnect,            // 수동 해제
  sendMessage,           // 메시지 전송
} = useWebSocket<SystemStats>(wsUrl, {
  autoConnect: true,           // 자동 연결 (기본값)
  reconnectAttempts: 5,        // 재연결 시도 횟수 (기본값)
  reconnectInterval: 3000,     // 기본 재연결 간격 (기본값)
  parseMessage: (data) => schema.safeParse(data).data,
  onMessage: (data) => { ... },
  onOpen: () => { ... },
  onClose: () => { ... },
  onError: (event) => { ... },
})
```

**핵심 기능**:
- **Latest Ref Pattern**: 콜백을 Ref에 저장하여 렌더링 사이클과 분리
- **Exponential Backoff**: `baseInterval * 2^retryCount` (최대 30초)
- **마운트 안전**: `isMountedRef`로 언마운트 후 상태 업데이트 방지

### LogTerminal

```typescript
import { LogTerminal } from '@/components/docker/LogTerminal'

<LogTerminal
  containerName="hololive-bot"
  onConnectionChange={(connected) => setConnected(connected)}
/>
```

**기능**:
- **ANSI 스트리핑**: ESC 시퀀스 (CSI, 0x9B) 제거
- **로그 파싱**: 정규식으로 타임스탬프, 레벨, 소스, 내용 분리
- **색상 하이라이팅**:
  - ERR/ERROR/FATAL/FTL → 빨강
  - WRN/WARN → 노랑
  - INF/INFO → 초록
  - DBG/DEBUG/TRC/TRACE → 하늘색
- **가상화 스크롤**: 5000줄 버퍼, `overscan: 20`
- **자동 스크롤**: `scrollToIndex(length - 1, { align: 'end' })`

### SystemStatsChart

```typescript
import { SystemStatsChart } from '@/components/dashboard/SystemStatsChart'

<SystemStatsChart />
```

**기능**:
- **Recharts AreaChart**: CPU (하늘색), 메모리 (보라색) 그래프
- **데이터 포인트**: 최대 30개 유지 (30초 히스토리)
- **Zod 파싱**: 숫자 타입 강제 변환 (`z.coerce.number()`)
- **애니메이션 비활성화**: 실시간 데이터에 적합
- **로딩 오버레이**: 2개 미만 데이터 포인트 시 표시
- **현재 값 표시**: CPU%, 메모리%, Goroutine 수

### UI 컴포넌트

```typescript
import { Button, Card, Badge, StatCard, Input, Label, Form } from '@/components/ui'

// Button
<Button variant="default" size="sm" disabled={isPending}>저장</Button>

// Card
<Card className="p-4">
  <Card.Header>헤더</Card.Header>
  <Card.Body>내용</Card.Body>
  <Card.Footer>푸터</Card.Footer>
</Card>

// Badge
<Badge variant="success">활성</Badge>
<Badge variant="destructive">비활성</Badge>

// StatCard
<StatCard
  title="멤버"
  value={42}
  icon={Users}
  onClick={() => navigate('/dashboard/members')}
/>
```

---

## 🔧 개발 가이드

### 경로 별칭

```typescript
// tsconfig.app.json & vite.config.ts에서 설정
'@/*'            → 'src/*'
'@/components/*' → 'src/components/*'
'@/pages/*'      → 'src/pages/*'
'@/api/*'        → 'src/api/*'
'@/stores/*'     → 'src/stores/*'
'@/types/*'      → 'src/types/*'
'@/lib/*'        → 'src/lib/*'
'@/hooks/*'      → 'src/hooks/*'
```

### 새 탭 추가하기

1. `src/components/NewTab.tsx` 생성
2. `App.tsx`에서 lazy import:
   ```typescript
   const NewTab = lazy(() => import('@/components/NewTab'))
   ```
3. 라우터에 경로 추가:
   ```typescript
   { path: "newtab", element: <LazyRoute><NewTab /></LazyRoute> }
   ```
4. `AppLayout.tsx`의 `navItems`에 추가:
   ```typescript
   { id: 'newtab', label: '새 탭', icon: SomeIcon, path: '/dashboard/newtab' }
   ```

### 새 API 엔드포인트 추가하기

1. `src/types/index.ts`에 타입 정의:
   ```typescript
   export interface NewData { ... }
   export interface NewDataResponse { status: string; data: NewData }
   ```

2. `src/api/index.ts`에 API 함수 추가:
   ```typescript
   export const newApi = {
     get: async () => {
       const response = await apiClient.get<NewDataResponse>('/new')
       return response.data
     },
     create: async (data: NewData) => {
       const response = await apiClient.post<ApiResponse>('/new', data)
       return response.data
     },
   }
   ```

3. 컴포넌트에서 사용:
   ```typescript
   const { data, isLoading } = useQuery({
     queryKey: ['new'],
     queryFn: newApi.get,
   })

   const mutation = useMutation({
     mutationFn: newApi.create,
     onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['new'] }),
   })
   ```

### 폼 유효성 검사 추가하기

```typescript
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'

const schema = z.object({
  name: z.string().min(1, "이름을 입력하세요"),
  value: z.coerce.number().min(0, "0 이상이어야 합니다").max(100),
})

type FormValues = z.infer<typeof schema>

const form = useForm<FormValues>({
  resolver: zodResolver(schema),
  defaultValues: { name: '', value: 0 },
})
```

### WebSocket 연결 추가하기

```typescript
const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
const wsUrl = `${protocol}//${window.location.host}/admin/api/ws/new-stream`

const { isConnected, lastMessage } = useWebSocket<DataType>(wsUrl, {
  parseMessage: (data) => dataSchema.safeParse(data).data || null,
  onMessage: (data) => { /* 데이터 처리 */ },
})
```

---

## ⚙️ TypeScript 설정

### 엄격 모드 (tsconfig.app.json)

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "strict": true,

    // ANY 금지 및 타입 우회 엄금
    "noImplicitAny": true,
    "noImplicitReturns": true,
    "noImplicitThis": true,
    "strictNullChecks": true,
    "strictFunctionTypes": true,
    "strictBindCallApply": true,
    "strictPropertyInitialization": true,
    "noUncheckedIndexedAccess": true,
    "noPropertyAccessFromIndexSignature": true,

    // 미사용 코드 금지
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "allowUnusedLabels": false,
    "allowUnreachableCode": false,
    "noFallthroughCasesInSwitch": true
  }
}
```

### 타입 안전성 유틸리티 (src/lib/typeUtils.ts)

외부 라이브러리의 `any`/`unknown` 반환값을 안전하게 처리:

```typescript
// unknown에서 에러 메시지 추출
extractErrorMessage(data: unknown): string | undefined

// unknown에서 문자열 속성 추출
extractStringProperty(data: unknown, key: string): string | undefined

// 타입 가드
hasProperty<K>(data: unknown, key: K): data is Record<K, unknown>

// catch 블록에서 에러 메시지 추출
getErrorMessageFromUnknown(error: unknown): string
```

---

## 📏 ESLint 설정

### Type-aware 규칙 (eslint.config.js)

```javascript
extends: [
  js.configs.recommended,
  tseslint.configs.recommendedTypeChecked,  // 타입 인식 규칙
  tseslint.configs.strictTypeChecked,        // 엄격한 타입 규칙
]
```

### ANY 금지 규칙

```javascript
rules: {
  '@typescript-eslint/no-explicit-any': 'error',
  '@typescript-eslint/no-unsafe-assignment': 'error',
  '@typescript-eslint/no-unsafe-member-access': 'error',
  '@typescript-eslint/no-unsafe-call': 'error',
  '@typescript-eslint/no-unsafe-return': 'error',
  '@typescript-eslint/no-unsafe-argument': 'error',
  '@typescript-eslint/no-non-null-assertion': 'error',
}
```

### ES6+ 규칙

```javascript
rules: {
  'no-var': 'error',
  'prefer-const': 'error',
  'prefer-arrow-callback': 'error',
  'prefer-template': 'error',
  'prefer-destructuring': 'error',
  'object-shorthand': ['error', 'always'],
  'arrow-body-style': ['error', 'as-needed'],
}
```

---

## 🚢 배포

### React Compiler

React 19 타겟으로 **React Compiler가 활성화**되어 있습니다:

```typescript
// vite.config.ts
plugins: [
  react({
    babel: {
      plugins: [['babel-plugin-react-compiler', { target: '19' }]],
    },
  }),
]
```

이를 통해 **자동 메모이제이션**이 적용되어 불필요한 리렌더링이 최소화됩니다.

### Docker 통합

Admin UI는 Go 백엔드의 Docker 이미지에 포함됩니다:

```dockerfile
# Dockerfile (hololive-kakao-bot-go)

# Frontend 빌드 스테이지
FROM node:20-alpine AS frontend-builder
WORKDIR /app/admin-ui
COPY admin-ui/package*.json ./
RUN npm ci
COPY admin-ui/ ./
RUN npm run build

# 최종 스테이지
FROM alpine:latest
COPY --from=frontend-builder /app/admin-ui/dist ./admin-ui/dist
# Go 서버가 /admin/* 경로에서 정적 파일 서빙
```

### SSR 데이터 주입

Go 서버가 경로별로 HTML에 데이터를 주입합니다:

```html
<!-- index.html (서버에서 수정) -->
<script>
  window.__SSR_DATA__ = {
    "members": {"status":"ok","members":[...]},
    "settings": {"status":"ok","settings":{...}}
  };
</script>
```

---

## 📚 추가 참고 자료

- [React 19 문서](https://react.dev)
- [React Compiler](https://react.dev/learn/react-compiler)
- [TanStack Query v5](https://tanstack.com/query/latest)
- [Vite 7](https://vite.dev)
- [TailwindCSS v4](https://tailwindcss.com)
- [shadcn/ui](https://ui.shadcn.com)
- [Recharts](https://recharts.org)

---
