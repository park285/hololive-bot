# 프론트엔드 개선 사항 기술 보고서 (Architecture, Performance, UX/UI)

본 보고서는 관리자 대시보드(Admin Dashboard) 프론트엔드 프로젝트에서 진행된 주요 개선 사항인 라우팅 시스템 중앙화, 성능 최적화, UX/UI 폴리싱에 대한 상세 기술 내용을 다룹니다.

---

## 1. 아키텍처 개선: 라우팅 시스템 중앙화 (Architecture)

### 문제점
기존 구조에서는 라우트 경로 정의, 사이드바 메뉴 구성, 그리고 컴포넌트 프리페칭 로직이 `App.tsx`, `AppLayout.tsx`, `prefetch.ts` 등 여러 파일에 분산되어 있어 유지보수가 어려웠습니다. 페이지 추가 시 여러 파일을 동시에 수정해야 했으며, 이는 경로 누락이나 불일치의 원인이 되었습니다.

### 해결책: Route Manifest 도입
`src/routes/manifest.ts`를 도입하여 모든 라우트 관련 정보를 한곳에서 관리하는 **Single Source of Truth(SSOT)** 패턴을 구현했습니다.

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
무거운 라이브러리(Recharts 등)를 사용하는 컴포넌트를 `lazy` 및 `Suspense`로 분리하여 초기 번들 사이즈를 최적화했습니다.
- **적용 대상**: `StatsTab` 내 `SystemStatsChart`, `ChannelStatsTable`.
- **효과**: 메인 번들 크기가 감소하였으며, **TBT(Total Blocking Time)**가 유의미하게 개선되었습니다.

### 하트비트 및 활동 감지 최적화
세션 유지를 위한 하트비트 로직을 `useHeartbeat` 커스텀 훅으로 분리하고 안정성을 강화했습니다.
- **경쟁 상태(Race Condition) 방지**: `AbortController`를 사용하여 새로운 요청 시 이전 요청을 즉시 취소합니다.
- **효과**: 불필요한 네트워크 부하를 줄이고, `visibilitychange` 이벤트를 통해 브라우저 탭 활성 상태에 따른 지능적인 요청 관리가 가능해졌습니다.

```typescript
// src/hooks/useHeartbeat.ts
const sendHeartbeat = useCallback(async (idle: boolean) => {
    if (abortControllerRef.current) {
        abortControllerRef.current.abort()
    }
    
    abortControllerRef.current = new AbortController()
    
    try {
        const response = await authApi.heartbeat(idle)
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
- **예시**: 사이드바 접기/펼치기 시 `transition-[width]`를 사용하여 레이아웃 변화에 필요한 연산만 수행하도록 제한했습니다.

---

## 4. UI 폴리싱 (Typography & Visuals)

### 타이포그래피 표준화
- **엘립시스(Ellipsis)**: 문법적으로 부정확한 세 개의 점(`...`)을 타이포그래피 표준 기호인 `…`으로 일괄 교체했습니다.
- **고정 폭 숫자(Tabular Numbers)**: 통계 데이터 및 테이블의 숫자 열에 `tabular-nums` 클래스를 적용하여 숫자가 변경될 때 텍스트가 흔들리는 현상을 방지하고 수직 정렬의 가독성을 높였습니다.

```html
<!-- src/components/ui/StatCard.tsx -->
<h3 className="text-3xl font-bold text-slate-800 tracking-tight tabular-nums">
  {value}
</h3>
```

---
**작성일**: 2026-01-20
**보고서 위치**: `admin-dashboard/docs/FRONTEND_IMPROVEMENTS.md`
