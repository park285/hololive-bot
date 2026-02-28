# Hololive k3s Cutover Runbook (Game-bot 제외)

- 작성일: 2026-02-27
- 대상: 단일 노드 k3s 운영 환경

## 0) 변경 범위

- 배포 대상: `valkey-cache`, `hololive-bot`, `alarm-dispatcher`, `stream-ingester`, `llm-scheduler`, `admin-api`
- 미포함: game-bot 계열, Postgres 내부화

## 1) Pre-deploy Checklist

- [ ] k3s 노드 자원 정상 (`CPU`, `Memory`, `Disk`)
- [ ] 이미지 pull 가능
- [ ] `deploy/k8s/hololive/base/configmap-app.yaml` 실값 반영 완료
- [ ] `deploy/k8s/hololive/base/secret-app.yaml` 실값 반영 완료
- [ ] `/srv/hololive/data` 생성
- [ ] 외부 Postgres 접근 확인 (Pod 기준)
- [ ] 변경 시간대 및 롤백 담당자 지정

### 필수 디렉터리 준비
```bash
sudo install -d -m 0770 -o 1000 -g 1000 /srv/hololive/data
```

## 2) 배포 절차

### 2-1. 전체 적용
```bash
kubectl apply -k deploy/k8s/hololive/overlays/prod
```

### 2-2. 상태 확인
```bash
kubectl -n hololive get pods -o wide
kubectl -n hololive get svc
```

## 3) 기능 검증

### 3-1. 앱 로그 확인
```bash
kubectl -n hololive logs deploy/hololive-bot --tail=200
kubectl -n hololive logs deploy/alarm-dispatcher --tail=200
kubectl -n hololive logs deploy/stream-ingester --tail=200
kubectl -n hololive logs deploy/llm-scheduler --tail=200
kubectl -n hololive logs deploy/admin-api --tail=200
```

> 로그 수집 정책: K8s prod 기본은 stdout-only (파일 로그 비활성)

### 3-2. 서비스 헬스체크
```bash
kubectl -n hololive port-forward svc/hololive-bot 30001:30001
# 다른 터미널
curl -fsS http://127.0.0.1:30001/health
```

```bash
kubectl -n hololive port-forward svc/admin-api 30002:30002
# 다른 터미널
curl -fsS http://127.0.0.1:30002/health
```

### 3-3. 오류 패턴 확인
다음 문자열이 반복되는지 점검합니다.
- `password authentication failed`
- `pool timed out`
- readiness/liveness probe failure

## 4) 선택 작업

### 4-1. DB 마이그레이션 Job 실행(필요 시)
```bash
kubectl apply -f deploy/k8s/hololive/optional/hololive-db-migrate-job.yaml
kubectl -n hololive logs job/hololive-db-migrate
```

### 4-2. VPN scraper proxy(필요 시)
```bash
kubectl apply -f deploy/k8s/hololive/optional/vpn-scraper-proxy-deployment.yaml
kubectl apply -f deploy/k8s/hololive/optional/vpn-scraper-proxy-service.yaml
```

## 5) 롤백 절차

### 5-1. k3s 워크로드 제거
```bash
kubectl delete -k deploy/k8s/hololive/overlays/prod
```

### 5-2. 기존 Docker 구성 복구
- 기존 compose 기반 hololive 앱 재기동
- 외부 Postgres는 변경 없으므로 데이터 롤백 불필요

## 6) 완료 판정

아래 조건을 모두 만족하면 cutover 완료로 판정합니다.
- 24시간 이상 Pod Ready 유지
- health endpoint 정상
- 알람/인제스트/스케줄러 핵심 기능 정상
- 치명 오류 패턴 미재현
