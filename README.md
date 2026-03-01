# Hololive k3s Migration (Game Bot 제외)

## 범위
- Kubernetes(k3s)로 이전:
  - `valkey-cache` (내부 Redis/Valkey)
  - `hololive-bot`
  - `alarm-dispatcher`
  - `stream-ingester`
  - `llm-scheduler`
  - `admin-api`
- 기존 Docker 유지:
  - PostgreSQL (`holo-postgres`)
  - game-bot 계열 전체

## 관련 계획/런북
- 계획서: `docs/HOLOLIVE_K3S_CUTOVER_PLAN_20260227.md`
- 실행 런북: `docs/runbook_execution/HOLOLIVE_K3S_CUTOVER_RUNBOOK_20260227.md`

## 사전 조건
1. k3s 노드에 다음 디렉터리 생성
```bash
sudo install -d -m 0770 -o 1000 -g 1000 /srv/hololive/data
sudo install -d -m 0770 -o 1000 -g 1000 /srv/hololive/logs
```
2. 외부 PostgreSQL 접근 가능해야 함
   - Pod에서 접근 가능한 IP/Port로 노출 필요
   - 로컬 전용(`127.0.0.1`) 바인딩이면 Pod에서 접근 불가
3. 이미지 준비
   - `hololive-kakao-bot-go:prod`
   - `hololive-alarm-dispatcher:prod`
   - `hololive-stream-ingester:prod`
   - `hololive-llm-scheduler:prod`
   - `hololive-admin-api:prod`

## 설정 파일 수정
- `base/configmap-app.yaml`
  - `POSTGRES_HOST` (권장: `holo-postgres` 유지)
  - `IRIS_BASE_URL`
  - `CLIPROXY_BASE_URL`
  - `SERVICES_*_HEALTH_URL`
  - `DOCKER_HOST`
- `base/host-external-services.yaml`
  - IP 직접 라우팅 모드 사용 시 EndpointSlice addresses(기본 `10.42.0.1`)만 수정
- `overlays/prod/patch-external-services-to-externalname.yaml` (기본 DNS 모드)
  - 기본값은 현재 호스트/도커 브리지 실주소를 `*.nip.io`로 사용
  - `llm-server`, `game-bot-*`는 Docker 컨테이너 IP 기반이므로 컨테이너 재생성 시 값 재확인 필요
- `base/secret-app.yaml`
  - DB 비밀번호/토큰/API 키 값 실값으로 교체

## 로그 정책 (K8s prod)
- SSOT: **stdout → Fluent Bit → Loki** 단일 경로
- 파일 로깅 제거됨 (hostPath 볼륨 없음, `LOG_DIR=""`, `FILE_ENABLED=false`)
- 로그 조회:
  - Grafana: `http://localhost:30090` (Loki 데이터소스)
  - CLI: `./scripts/logs/tail.sh <service>` (실시간), `./scripts/logs/query.sh <service>` (범위 조회)
  - kubectl: `kubectl -n hololive logs deploy/<name>` (kubelet 버퍼, 보조용)

## 배포
```bash
# 기본(DNS ExternalName 모드, monitoring 포함)
kubectl kustomize k8s/overlays/prod --enable-helm | kubectl apply --server-side -f -

# 대체(IP EndpointSlice 직접 라우팅 모드)
kubectl kustomize k8s/overlays/prod-ip --enable-helm | kubectl apply --server-side -f -

# 호환 별칭(= 기본과 동일)
kubectl kustomize k8s/overlays/prod-dns --enable-helm | kubectl apply --server-side -f -
```

## 검증
```bash
kubectl -n hololive get pods
kubectl -n hololive get svc
kubectl -n hololive logs deploy/hololive-bot --tail=200
kubectl -n hololive logs deploy/stream-ingester --tail=200
kubectl -n hololive logs deploy/admin-api --tail=200
```

## 마이그레이션 Job(선택)
- 호스트 경로 `/home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go/scripts/migrations` 기준 템플릿입니다.
- 경로가 다르면 `optional/hololive-db-migrate-job.yaml` 수정 후 실행하십시오.
```bash
kubectl apply -k k8s/optional
kubectl -n hololive logs job/hololive-db-migrate
```

## 롤백
```bash
# 기본 모드 롤백
kubectl kustomize k8s/overlays/prod --enable-helm | kubectl delete -f -

# 대체(IP EndpointSlice) 모드 롤백(사용한 경우)
kubectl kustomize k8s/overlays/prod-ip --enable-helm | kubectl delete -f -

# 호환 별칭 롤백(= 기본과 동일)
kubectl kustomize k8s/overlays/prod-dns --enable-helm | kubectl delete -f -

# 필요 시 optional도 제거
kubectl delete -k k8s/optional
```

## 리소스 메모
- 본 구성의 메모리 limit 합(hololive + valkey): 약 **2.13GiB**
- k3s/system 오버헤드 포함 예상: 약 **3.1~4.1GiB**
- 현재 호스트(54Gi RAM, 12 vCPU) 기준으로는 여유가 큰 편입니다.

## 로깅 스택 (Fluent Bit + Loki + Grafana)

중앙 로그 수집/검색 스택. hololive 네임스페이스 Pod 로그를 Fluent Bit → Loki로 수집, Grafana로 검색.

### 구성
| 컴포넌트 | 유형 | 리소스 | 스토리지 |
|---------|------|--------|---------|
| Fluent Bit | DaemonSet | 50m/128Mi | — |
| Loki (SingleBinary) | StatefulSet | 100m/512Mi | 10Gi PVC |
| Grafana | Deployment | 50m/256Mi | 1Gi PVC |

### 배포 (--enable-helm 필수)
```bash
# kustomize 빌드 검증
kubectl kustomize k8s/overlays/prod --enable-helm | head -100

# 배포 (kubectl apply -k는 --enable-helm 미지원 → 파이프 사용)
kubectl kustomize k8s/overlays/prod --enable-helm | kubectl apply --server-side -f -
```

### 검증
```bash
# Pod 상태
kubectl -n hololive get pods -l 'app.kubernetes.io/name in (loki,fluent-bit,grafana)'

# Fluent Bit → Loki 연결
kubectl -n hololive logs ds/fluent-bit | grep -i loki

# Grafana 접속 (NodePort 30090)
curl -s http://localhost:30090/api/health | jq .

# Loki 쿼리 테스트
curl -G -s http://localhost:30090/api/datasources/proxy/1/loki/api/v1/labels | jq .
```

### 접속 정보
- **Grafana**: `http://<node-ip>:30090`
- **기본 계정**: admin / (첫 로그인 시 변경)
- **Loki 데이터소스**: 자동 프로비저닝 (http://loki.hololive.svc.cluster.local:3100)
- **보관 기간**: 14일
