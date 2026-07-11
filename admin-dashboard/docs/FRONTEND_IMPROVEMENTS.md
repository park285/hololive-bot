# 프론트엔드 개선 사항 기술 보고서 (Architecture, Performance, UX/UI)

관리자 대시보드(Admin Dashboard) 프론트엔드에서 진행한 주요 개선은 라우팅 시스템 중앙화, 성능 최적화, UX/UI 폴리싱 세 가지입니다. 각 개선의 기술 내용을 아래에 정리합니다.

---

## 1. 아키텍처 개선: 라우팅 시스템 중앙화 (Architecture)

### 문제점
기존 구조에서는 라우트 경로 정의, 사이드바 메뉴 구성, 컴포넌트 프리페칭 로직이 `App.tsx`, `AppLayout.tsx`, `prefetch.ts` 등 여러 파일에 흩어져 있어 유지보수가 어려웠습니다. 페이지를 추가할 때마다 여러 파일을 동시에 고쳐야 했으므로 경로가 누락되거나 어긋나기 쉬웠습니다.

### 해결책: Route Manifest 도입
`src/routes/manifest.ts`를 도입해 모든 라우트 정보를 한곳에서 관리하는 **Single Source of Truth(SSOT)** 패턴을 구현했습니다.

#### 기술 상세
- **`ROUTE_MANIFEST`**: 경로(path), 아이콘(icon), 라벨(label), 그룹(group), 동적 로드 함수(load)를 포함하는 단일 설정 배열입니다.
- **헬퍼 함수 구현**:
  - `getNavGroups()`: 사이드바 그룹화 로직을 캡슐화합니다.
  - `prefetchRoute(id)`: 메뉴 호버 시 해당 청크를 미리 로드하여 응답 속도를 향상시킵니다.
  - `getLazyComponent(id)`: 라우트 ID별 `lazy` 컴포넌트 생성을 자동화하고 캐싱합니다.

#### 구현 예시 (src/App.tsx)
```typescript
children: [
    {
        index: true,
        element: <Navigate to="stats" replace />
    },
    ...ROUTE_MANIFEST.map(route => {
        const Component = getLazyComponent(route.id);
        return {
            path: route.path,
            element: (
                <LazyRoute>
                    <Component />
                </LazyRoute>
            )
        };
    })
]
```

---

## 2. 성능 최적화 (Performance)

### 코드 분할 (Code Splitting)
차트는 별도 차트 라이브러리 없이 손으로 작성한 SVG(`frontend/src/features/stats/components/ResourceChart.tsx`, `RuntimeUnitsChart.tsx`)로 렌더링합니다. 이런 무거운 컴포넌트를 `lazy`와 `Suspense`로 분리해 초기 번들 사이즈를 줄였습니다.
- **적용 대상**: `ResourceChart`, `RuntimeUnitsChart` 등 stats 차트 컴포넌트.
- **효과**: 메인 번들 크기가 줄었고 **TBT(Total Blocking Time)**가 유의미하게 개선되었습니다.

### 하트비트 및 활동 감지 최적화
세션 유지를 위한 하트비트 로직을 `useHeartbeat` 커스텀 훅으로 분리하고 안정성을 강화했습니다.
- **경쟁 상태(Race Condition) 방지**: `AbortController`로 새 요청 시 이전 요청을 즉시 취소합니다.
- **Idle 전환 즉시 반영**: `isIdle=true`가 되면 다음 정기 주기를 기다리지 않고 즉시 heartbeat를 전송합니다.
- **서버 idle 거부 즉시 이탈**: `idle_rejected` 응답 수신 시 즉시 로그아웃하여 서버 측 10초 TTL 단축과 프론트 상태를 맞춥니다.
- **후속 pre-warning 분리 지원**: backend가 `/auth/session`에 `absolute_expires_at`와 `session_policy`를 제공하므로, UI 레이어는 별도 작업으로 pre-warning 모달만 구현하면 됩니다.
- **효과**: 불필요한 네트워크 부하를 줄이고 `visibilitychange` 이벤트로 브라우저 탭 활성 상태에 맞춰 요청을 지능적으로 관리합니다.

```typescript
// src/hooks/useHeartbeat.ts
const sendHeartbeat = useCallback(async (idle: boolean) => {
    if (abortControllerRef.current) {
        abortControllerRef.current.abort()
    }
    
    abortControllerRef.current = new AbortController()
    
    try {
        const response = await authApi.heartbeat(idle, controller.signal)
        // ... 생략
    } catch (e: any) {
        if (e.name === 'AbortError') return
        // ... 생략
    }
}, [logout])
```

---

## 3. UX 및 접근성 개선 (UX & Accessibility)

### 접근성(A11y) 강화
- **명시적 레이블**: 사이드바 토글, 로그아웃 등 아이콘만 있는 버튼에 `aria-label`을 추가했습니다.
- **키보드 접근성**: 인터랙티브 요소에 `role="button"` 및 적절한 `tabIndex`를 부여하고 키보드 이벤트 핸들러를 추가했습니다.

### CSS 트랜지션 최적화
브라우저의 리페인팅 부하를 줄이기 위해 `transition-all`을 지양하고 구체적인 속성을 명시했습니다.
- **예시**: 사이드바 접기/펼치기 시 `transition-[width]`를 지정해 레이아웃 변화에 필요한 연산만 수행하도록 제한했습니다.

---

## 4. UI 폴리싱 (Typography & Visuals)

### 타이포그래피 표준화
- **엘립시스(Ellipsis)**: 문법적으로 부정확한 세 개의 점(`...`)을 타이포그래피 표준 기호인 `…`으로 일괄 교체했습니다.
- **고정 폭 숫자(Tabular Numbers)**: 통계 데이터와 테이블의 숫자 열에 `tabular-nums` 클래스를 적용해 숫자가 바뀔 때 텍스트가 흔들리는 현상을 막고 수직 정렬 가독성을 높였습니다.

```html
<!-- src/components/ui/StatCard.tsx -->
<h3 className="text-3xl font-bold text-slate-800 tracking-tight tabular-nums">
  {value}
</h3>
```

---
**작성일**: 2026-01-20
**보고서 위치**: `admin-dashboard/docs/FRONTEND_IMPROVEMENTS.md`
