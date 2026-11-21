# 중앙 Quadlet unit

중앙 호스트(hololive-seoul, `deploy/topology/hosts.conf`의 `central`)는 rootful Podman과 systemd
Quadlet으로 운영합니다. iris-seoul collector b는 같은 방식의 별도 unit([deploy/podman/iris-seoul](../podman/iris-seoul/README.md))이며
배포·롤백은 중앙 c와 같은 `po-central-cutover.sh`(slot `b`)가 소유합니다.

## 구성

| 경로 | 내용 |
| --- | --- |
| `hololive-bot/` | 중앙 운영 unit. 호스트의 `/etc/containers/systemd/hololive-bot/`에 설치합니다 |
| `optional/x-spaces/` | `HOLOLIVE_X_SPACES_ENABLED=1`일 때만 설치하는 drop-in(`40-x-spaces.conf`) |
| `optional/schedule-poster/` | `HOLOLIVE_SCHEDULE_POSTER_ENABLED=1`일 때만 설치하는 hololive-api drop-in(`41-schedule-poster.conf`) |
| `optional/schedule-poster-review/` | `HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED=1`일 때만 설치하는 hololive-api drop-in(`42-schedule-poster-review.conf`, 후보 자동 검토용 Gemini API 키) |
| `optional/broadcast-type-decision/` | `HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED=1`일 때만 설치하는 hololive-api drop-in(`43-broadcast-type-decision.conf`, 미분류 방송 유형 판정용 Gemini API 키) |

`optional/`은 설치 트리 밖에 둡니다. Quadlet은 같은 검색 루트 아래 형제 디렉터리의
`<unit>.container.d/` drop-in도 적용하므로, 선택 drop-in을 `hololive-bot/` 옆에 두면 항상 적용됩니다.

| unit | 컨테이너 | 비고 |
| --- | --- | --- |
| `holo-postgres.container` | `holo-postgres` | 권위 primary. `admin-dashboard-ingress-firewall.service`를 Requires |
| `valkey-cache.container` | `valkey-cache` | Valkey HA hololive 그룹 노드. 역할은 `/data/valkey.conf`가 소유 |
| `hololive-db-migrate.container` | `hololive-db-migrate` | one-shot. `[Install]` 없음 |
| `hololive-alarm-worker.container` | `hololive-alarm-worker` | 알림 dispatcher(단일 인스턴스) |
| `hololive-api.container` | `hololive-api` | bot/admin/YouTube plane |
| `hololive-youtube-po-c.container` | `hololive-youtube-po-c` | collector c의 격리 PO issuer |
| `hololive-youtube-collector-c.container` | `hololive-youtube-collector-c` | collector fleet c. network alias `youtube-collector` |
| `admin-dashboard-ingress.container` | `admin-dashboard-ingress` | host network shortlink ingress |
| `hololive-net.network` | `hololive-bot_hololive-net` | 172.23.0.0/16, interface `pm-holo` |

## 설치

배포할 commit의 clean checkout에서 root로 실행합니다. `/opt/hololive-bot/compose/current`는 `git archive` 결과라
git 저장소가 아니므로 거기서 실행하면 `repo git tree missing`으로 멈춥니다.

```bash
sudo ./scripts/deploy/sync-opt-current.sh
```

기존 운영 데이터·설정·권한을 바꾸지 않는 artifact-only 갱신에는
`sudo ./scripts/deploy/sync-opt-current.sh --preserve-runtime`을 사용합니다. 이 모드는 기존
`data`·`logs`·`backups`·`runtime-config` 디렉터리가 있어야 하며, runtime 입력을 backup/seed하지 않고
보호 디렉터리와 worker-profile 저장소의 bytes·owner·mode를 유지합니다. 기본 전체 동기화의 시드·권한 준비는
그 효과까지 승인된 설치에만 사용합니다. 두 모드 모두 새 고유 exec-tree 복구점을 만들며 기존 backup을
덮거나 보존 개수만으로 삭제하지 않습니다. 과거 복구점 정리는 별도 승인 절차입니다.

### 보호 인프라를 유지하는 중앙 앱 소스 게시

이미 `--schema-only`인 중앙에 앱 소스만 교체할 때는 `--protected-source`를 사용합니다. `--application-only`는 최초 풀링 후 카드 전환용이며, `--source-only OLD`는 기존 일반 파일의 제한된 수정만 허용합니다. 두 옵션이나 전체 동기화를 실패 시 대안으로 실행하지 않습니다.

kapu에서 도구와 후보의 전체 SHA, bundle SHA-256, **실행할 게시 스크립트 자체의 SHA-256**, API·worker의 검증된 linux/arm64 이미지 ID를 승인 목록에 고정합니다. 중앙 root가 `/opt/hololive-bot/release-sources/<고유 이름>`을 `0700`으로 새로 만들고 승인 목록과 전송 바이트를 대조합니다. 빈 `source`를 `git init --template=`로 초기화한 뒤 검증한 bundle을 로컬 경로로 fetch하여 정확한 커밋을 detached checkout합니다. 외부 Git 설정(include, hook, fsmonitor, filter, alternate object 등)이나 다른 사용자가 준비한 `.git`을 가져오지 않습니다. 통합본이면 도구와 후보에 같은 clone을 쓸 수 있습니다. `$CANDIDATE/{hololive-api,hololive-alarm-worker}.candidate-image`는 승인된 해당 `sha256:<64자리 ID>` 한 줄씩입니다. 중앙에서는 빌드하지 않습니다.

신뢰의 시작점은 checkout 안의 자체 검사나 디렉터리 이름이 아니라 **운영 root가 준비한 비공개 경로와 외부 승인 목록**입니다. 다음 진입부는 checkout을 실행하기 전에 경로 소유권·권한·ACL·단일 링크와 kapu에서 승인된 스크립트 해시를 확인합니다. 이어 게시자가 도구·후보의 배포/복구 coordinator, prestart, 봉인 권한·프로필 도구 7개 blob과 실행 비트를 비교하고 실제 로드된 양쪽 이미지의 플랫폼·제품·전체 revision을 대조합니다. 봉인 세대를 실행하지 못하는 v20.0.0은 중간 단계가 아닙니다.

```bash
sudo -n /usr/bin/env -i PATH=/usr/sbin:/usr/bin HOME=/root LC_ALL=C \
  /usr/bin/python3 -ISB - "$TOOL" "$BOOTSTRAP_SHA256" "$INSTALLED_SHA" "$CANDIDATE_SHA" "$TOOL_SHA" "$CANDIDATE/source" <<'PY'
import hashlib, os, stat, sys
from pathlib import Path

stage = Path(sys.argv[1])
source = stage / "source/scripts/deploy/sync-opt-current.sh"
if stage.parent != Path("/opt/hololive-bot/release-sources") or stage.stat().st_mode & 0o7777 != 0o700:
    sys.exit("untrusted tool stage")
for p in [*reversed(source.parents), source]:
    s = p.lstat()
    kind = (stat.S_ISREG(s.st_mode) and s.st_nlink == 1) if p == source else stat.S_ISDIR(s.st_mode)
    if not kind or s.st_uid or s.st_gid or s.st_mode & 0o7022 or any(
            k.startswith("system.posix_acl_") or k == "security.capability" for k in os.listxattr(p, follow_symlinks=False)):
        sys.exit("untrusted bootstrap path")
with source.open("rb") as f:
    if hashlib.file_digest(f, "sha256").hexdigest() != sys.argv[2]:
        sys.exit("unapproved bootstrap bytes")
os.execv("/usr/bin/bash", ["bash", str(source), "--protected-source", *sys.argv[3:]])
PY
```

이 모드는 기존 중앙 배포 lock을 잡고 **별도의 앱 코드 세대**를 만듭니다. `/opt/hololive-bot/compose/app-sources`가 처음부터 없을 때만 `current/.source-revision`과 구 커밋의 실행 소스를 대조합니다. 구 `current` 안의 root:root 그룹 쓰기 가능 파일은 실행 권한의 근거가 아닙니다. 소스 바이트·실행 비트·실측 메타데이터를 읽어 대조하고 보관할 뿐, 거기서 코드를 실행하거나 파일을 쓰거나 지우지 않습니다. 이후에는 `app-sources/active`가 가리키는 봉인 세대가 유일한 출발점입니다. 후보의 파일 추가·변경·제거는 새 archive에만 반영되며 `current`의 세대·데이터·프로필·설정은 그대로입니다.

설치된 Quadlet·prestart·방화벽, idle schema migrator, 건강한 PostgreSQL·풀러와 실제 읽기 전용 mount를 게시 전후에 확인합니다. PG HBA와 풀러 HBA·stats-users는 실행 중인 파일과 다음 기동의 호스트 파일이 다른 inode일 수 있습니다. 이 경우에만 양쪽 바이트·mode·소유권·SELinux 등 xattr가 같아야 하며, 양쪽 device/inode를 따로 고정합니다. 불일치·ACL·예상 밖 mode는 중단 사유입니다. 권한 보정, host unit 재설치·reload, 서비스 제어, SQL 실행은 하지 않습니다.

기존 `/usr/local/libexec/hololive-bot/quadlet-prestart`와 `lib/*.sh`의 정본은 **설치 당시 `current/.source-revision`의 Git 객체**입니다. 실제 설치 파일과 기존 `current` 사본이 서로 같아도 둘 다 그 객체와 다르면 거부합니다. 반대로 봉인할 앱의 prestart와 라이브러리는 **후보 커밋의 Git 객체**와 대조합니다. 후보의 endpoint 판정 코드가 바뀌어도 설치 libexec를 교체하거나 그 권한을 바꾸지 않습니다. 이 구분은 보호 소스 게시 전용이며, 방화벽·Quadlet·기존 배포 모드의 일치 조건은 그대로입니다.

kapu의 `bash scripts/deploy/sync-opt-current_test.sh`는 격리 fixture에서 서로 다른 선행·후보 helper를 게시하고, 두 선행 파일이 똑같이 변조된 경우와 검증 뒤 staging 바이트가 바뀐 경우를 거부하는지 검사합니다. 인자 없는 실행은 먼저 `--unstarted-source` 검사를 하므로 `d92cf34`의 첫 first-parent 자식이면서 승인된 15경로 변경과 정확히 같은 복구 커밋, `6d7e874`·`efafcf40` 이력, `uv`, `unshare --uts`가 필요합니다. `HOLOLIVE_TEST_841_OBSERVER`에 실제 #841 관찰자 파일을 지정하면 fixture 대신 그 파일로 원래 시도를 재현합니다. 전체 #837→#840 복원 검증은 두 커밋의 이력이 있는 로컬 checkout에서 `bash scripts/deploy/sync-opt-current_test.sh --released-predecessor`로 실행합니다. 이 명시적 검사는 정식 구 archive·설치 자산과 전체 후보 Git 트리를 쓰며 운영 호스트·원격 Git·DB를 조회하지 않습니다.

성공하면 `app-sources/active`(root:root `0600`)에 전체 SHA 한 줄이 남습니다. 서비스 전환은 **명시한 후보 SHA**, `active`, `app-sources/<SHA>/.source-revision`(`0400`), 이미지 revision을 일치시켜야 합니다. 세대 루트는 `0500`, 하위 디렉터리와 실행 파일은 `0555`, 일반 파일은 `0444`입니다. 코드·manifest·lineage SQL은 이 세대에서, 실행 데이터·프로필·인프라 bind는 기존 `current`에서 읽습니다. 둘을 잇는 심링크나 `current/.source-revision` 덮어쓰기는 없습니다. 소스 게시 성공은 DB 검증이나 앱 기동 성공을 뜻하지 않습니다.

후속 게시는 양쪽 실제 API·alarm-worker의 healthy 이미지, root 소유 release 영수증과 `schema-check` 종료 증거, 승인된 `50-migrator-image.conf`의 `--check`와 봉인 prestart를 요구합니다. 각 서비스의 보관 pair·설치 unit/drop-in·OCI의 실제 읽기 전용 mount와 인증된 진단의 **로드된 프로필 해시**를 검증합니다. 파일 이름이나 healthy 상태만으로 수용을 추정하지 않습니다. 설치되지 않은 인프라를 보정하거나 프로필 내용을 출력하지 않습니다.

`not_activated stage=migration`은 앱이 교체되지 않았다는 뜻이지 설치 파일이 모두 원래 상태라는 뜻은 아닙니다. schema job을 요청하기 전에도 `50-migrator-image.conf`를 교체하고 `daemon-reload`할 수 있습니다. 원 요청의 release, 양쪽 job 영수증과 manager 기록, 현재 migrator의 실제 image·`--check`, 두 앱의 실행 세대와 프로필을 따로 대조합니다. 앱과 게시 세대가 다르고 `schema-check`가 없다면 정상 후속 게시의 전제는 충족되지 않습니다.

최초 봉인 소스만 게시되고 API의 원래 요청이 schema job을 발행하기 전에 종료된 경우에는 `scripts/deploy/supersession-window.py`를 사용합니다. 먼저 운영 root가 관리하는 `dv-tool-<NEW>/source`와 두 이미지 stage의 공식 출처를 검증합니다. 위 bootstrap과 같은 방식으로 **실행할 supervisor 자체**의 경로·소유권·ACL·단일 링크·SHA-256을 외부 승인값과 대조합니다. `OLD`는 게시된 소스, `RUNNING`은 실제 두 writer, `FAILED_API_RELEASE`는 실패한 원래 API release의 디렉터리 이름입니다. 마지막 두 인자는 새 API·worker 수송 manifest의 승인된 SHA-256입니다.

```
python3 -ISB "$TOOL/source/scripts/deploy/supersession-window.py" PHASE OLD NEW RUNNING FAILED_API_RELEASE API_MANIFEST WORKER_MANIFEST
```

먼저 같은 승인 인자로 `PHASE=precheck`를 실행합니다. #841 도구가 남긴 원래 영수증의 내용 형식은 중앙의 실제 파일로만 확인되므로 이 단계를 건너뛰지 않습니다. 아래 SSH bootstrap 블록에서 `phase=precheck`로 설정하고 `"${ssh_window[@]}" </dev/null`의 성공을 확인합니다. 이 읽기 전용 검사는 잠금 안에서 manifest·clone·봉인 트리·원래 영수증과 실제 중앙 저널을 대조합니다. 예약·게시·서비스 요청은 발생하지 않습니다. source 창은 별도 승인 후 tool stage 밖의 `release-sources/source-OLD-NEW.requested`와 tool stage에 함께 예약하고 같은 증거를 다시 검사합니다. 둘 중 하나라도 남아 있으면 precheck와 source 창을 다시 열 수 없으므로 tool stage를 백업으로 복원해도 첫 시도를 되풀이하지 않습니다. api·worker·receipt 창은 직전 입력의 원본을 인증하고 deploy 잠금을 잡은 뒤 해당 stage와 중앙 transition에 각각 사용 기록을 남기므로, 다른 배포가 잠금을 잡고 있으면 기록 없이 거부됩니다. 어느 한쪽에 기록이 남아 있으면 창을 다시 열 수 없습니다.

`PHASE`는 `source`, `api`, `worker`, `receipt` 순서입니다. 각 창은 독립 승인하고 직전 창의 성공을 확인합니다. supervisor가 원 요청을 기록한 다음 실제 게시기(`--supersede-unstarted`)나 봉인 coordinator를 단 한 번 호출하고 종료 결과를 보존합니다. 하위 명령만 직접 실행하거나 `source.accepted`·`activation.accepted` 파일을 수동으로 만들지 않습니다. 예상 SHA가 이미 보이더라도 원 요청이 불명확하면 다음 단계로 넘어가지 않습니다.

각 창에서 kapu는 원래 SSH 프로세스의 stdout 원본과 실제 종료 코드를 함께 보존합니다. 중앙 supervisor는 매번 새 nonce를 stdout에 넣습니다. source·api·worker 창은 응답의 SHA-256을 기록하고, receipt 창은 응답 전체와 worker 입력을 `activation.confirmed`에 함께 기록합니다. 다음 창은 원본 응답에 정수 `process_exit` 값 0을 추가한 정확한 바이트만 받습니다. 중앙에서 검증할 수 있는 것은 원본 응답을 받았다는 사실까지입니다. SSH 종료 코드는 kapu 운영자가 원래 프로세스에서 관찰해 증언합니다.

중앙 root가 영수증 내용과 타임스탬프를 의도대로 위조하는 공격은 보호 범위 밖입니다. 운영자의 잘못된 stdin·재시도·동시 게시, 파일 복원과 부분 복사, 프로세스 중단·신호·시간 초과, systemd의 job 병합·자동 재시작·대기 job과 journald의 유실·쓰기 실패·커널 전달은 검증 대상입니다.

kapu에서 승인된 창 하나마다 아래 두 블록을 같은 Bash에서 실행합니다. `central`은 승인된 중앙 root SSH 목적지입니다. `phase`, `OLD`, `NEW`, `RUNNING`, `FAILED_API_RELEASE`, `API_MANIFEST`, `WORKER_MANIFEST`, `SUPERVISOR_SHA256`은 해당 창에서 승인한 값입니다. `evidence`는 이 시도 전용 보관 디렉터리입니다. `input`은 source 창에서 `/dev/null`, 이후에는 직전 창의 `$evidence/<직전 창>/transport`입니다.

```bash
remote_command() {
  uv run --no-project python - "$SUPERVISOR_SHA256" "$1" "$OLD" "$NEW" "$RUNNING" "$FAILED_API_RELEASE" "$API_MANIFEST" "$WORKER_MANIFEST" <<'PY'
import shlex, sys

bootstrap = """import hashlib, os, re, stat, sys
from pathlib import Path
seal, phase, old, new, *tail = sys.argv[1:]
if not re.fullmatch('[0-9a-f]{40}', new):
    sys.exit(75)
stage = Path('/opt/hololive-bot/release-sources') / ('dv-tool-' + new)
path = stage / 'source/scripts/deploy/supersession-window.py'
for p in [*reversed(path.parents), path]:
    s = p.lstat()
    kind = stat.S_ISREG(s.st_mode) and s.st_nlink == 1 if p == path else stat.S_ISDIR(s.st_mode)
    if not kind or s.st_uid or s.st_gid or s.st_mode & 0o7022 or any(k.startswith('system.posix_acl_') or k == 'security.capability' for k in os.listxattr(p, follow_symlinks=False)):
        sys.exit(75)
with path.open('rb') as f:
    if stat.S_IMODE(stage.stat().st_mode) != 0o700 or hashlib.file_digest(f, 'sha256').hexdigest() != seal:
        sys.exit(75)
os.execv(sys.executable, [sys.executable, '-ISB', str(path), phase, old, new, *tail])
"""
print(shlex.join(['/usr/bin/python3', '-ISB', '-c', bootstrap, *sys.argv[1:]]))
PY
}
remote=$(remote_command "$phase") || exit 75
ssh_window=(ssh -T -- "$central" "$remote")
```

```bash
(
  set -eC
  umask 077
  attempt=$evidence/$phase
  mkdir -m 0700 -- "$attempt" || exit 75
  for old_record in "$evidence/$phase."{stdout,process_exit,transport}; do
    [[ ! -e $old_record && ! -L $old_record ]] || exit 75
  done
  sync -f "$evidence" || exit 75
  rc=0
  "${ssh_window[@]}" <"$input" >"$attempt/stdout" || rc=$?
  printf '%s\n' "$rc" >"$attempt/process_exit" || exit 75
  sync -f "$attempt" || exit 75
  (( rc == 0 )) || exit 75
  if [[ $phase == receipt ]]; then
    readback=$(remote_command receipt-check) || exit 75
    ssh -T -- "$central" "$readback" <"$attempt/stdout" || exit 75
  fi
  uv run --no-project python - "$attempt" "$phase" "$input" "$OLD" "$NEW" "$RUNNING" "$FAILED_API_RELEASE" "$API_MANIFEST" "$WORKER_MANIFEST" <<'PY' || exit 75
import hashlib, json, os, re, sys, uuid
from pathlib import Path

directory, phase, original, old, new, running, failed, *seals = sys.argv[1:]
directory = Path(directory)
def encode(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode()
def oid(value, length):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{%d}' % length, value) and int(value, 16) != 0

raw = (directory / 'stdout').read_bytes()
response = json.loads(raw)
if encode(response) != raw:
    sys.exit(75)
if phase == 'receipt':
    if (not isinstance(response, dict) or set(response) != {'confirmed','worker_transport_sha256','output_nonce'}
        or response['confirmed'] != 'worker' or not oid(response['output_nonce'],32)
        or response['worker_transport_sha256'] != hashlib.sha256(Path(original).read_bytes()).hexdigest()):
        sys.exit(75)
    name, data = 'success', raw
else:
    hashes = {'accepted_sha256','window_sha256'} | ({'snapshot_sha256'} if phase == 'source' else set())
    if not isinstance(response, dict) or not isinstance(response.get('identity'),dict):
        sys.exit(75)
    identity = response['identity']
    boot = str(uuid.UUID(identity['boot']))
    if (response.keys() != hashes | {'identity','phase','window_nonce','output_nonce'} or response['phase'] != phase or
        not all(oid(response[k],64) for k in hashes) or not all(oid(response[k],32) for k in ('window_nonce','output_nonce')) or
        identity != dict(old=old,new=new,running=running,failed=failed,manifests=seals,boot=boot)):
        sys.exit(75)
    name, data = 'transport', encode(dict(response,process_exit=0))
pending = directory / 'success.pending'
with pending.open('xb') as stream:
    stream.write(data)
    stream.flush()
    os.fsync(stream.fileno())
pending.rename(directory / name)
fd = os.open(directory,os.O_DIRECTORY)
try:
    os.fsync(fd)
finally:
    os.close(fd)
PY
)
```

종료 코드를 잃었거나 stdout이 잘렸거나 수신·보관에 실패하면 결과는 `outcome_unknown`입니다. 원본을 보존하고 재시도와 다음 창 승인을 중단합니다. supervisor는 거부하면 종료 코드 75로 끝나고, 검증기의 고정 거부 사유가 있으면 그 사유를 stderr에 함께 출력합니다. `receipt` 응답에는 worker transport 원본의 SHA-256과 이 확인에서 새로 만든 nonce가 들어갑니다. kapu는 입력 해시와 실제 SSH 종료 0을 확인하고, 읽기 전용 receipt-check로 nonce를 포함한 stdout 전체가 중앙의 activation.confirmed와 일치할 때 성공을 기록합니다. `receipt` 창은 새 job 없이 두 슬롯의 요청·release·writer 기동·종료·수락 순서와 readiness, 앞선 전송 체인을 대조합니다. /opt와 deploy 잠금을 함께 유지합니다. 확인 응답을 stdout에 쓴 뒤 마지막 job·컨테이너 구간을 스캔합니다. 이어 양쪽 readiness·실행 신원과 보호 unit의 대기 job, 마지막 경계 이후 저널 구간, 두 예약 원본의 바이트를 다시 확인하고 마지막 커서가 30초 이내일 때 `activation.confirmed`를 마지막에 공개하고 디렉터리를 동기화합니다. kapu는 이 창에서도 정확한 응답과 SSH 종료 코드 0을 모두 확인해야 후속 게시를 승인할 수 있습니다.

kapu의 원래 `activation.reconcile`에는 다섯 영수증의 SHA-256, inode, ctime, mtime이 보존돼 있습니다. 이 사고에 대해 승인한 다섯 행의 해시가 검증기에 들어 있습니다. 중앙에서 현재 파일을 읽어 같은 해시를 얻어야 진단 시각을 실패 release의 상한으로 사용합니다. Linux 파일시각이 요청에 기록한 실시간보다 앞서는 경우에는 커널의 coarse realtime 시계 한 틱까지만 허용합니다. #841 coordinator는 요청 파일을 동기화한 뒤에만 schema job을 발행합니다. 고정한 원 영수증과 release에는 그 요청이 없습니다. 원래 구간에서 입증하는 것은 schema와 writer의 추가 실행이 없었고 원장과 두 writer가 유지됐다는 사실입니다. 실행 전에 취소된 외부 job은 효과가 없으며 원래 저널로 그 부재를 입증하지 않습니다. 현재 journald invocation의 시작 시각이 부재 구간의 첫 커서보다 늦거나 검사 중 invocation이 바뀌면 수락하지 않습니다. 원래 구간 안에서 PID 1의 INFO 등급 reload 완료 기록, 커서 전체의 rootful Podman start·died 이벤트와 manager 기록을 확인합니다. 원래 요청 구간의 manager 기록과 이후 같은 init.scope 우선순위 그룹의 기록을 확인하고, 원래 요청에서 rate limit에 도달할 가능성이 있으면 중단합니다. 원래 요청 시각보다 journald `RateLimitIntervalSec`만큼 앞선 마지막 PID 1 기록부터 현재까지 어느 unit에서든 journald의 메시지 억제·쓰기 실패 기록이 있어도 중단합니다. 저널 스캔은 400만 행과 90초를 넘으면 중단하므로, 원래 시도 뒤 저널이 크게 늘기 전에 창을 진행해야 합니다. 손상된 다른 저널 파일에 대한 `journalctl` 경고는 판정에 쓰지 않고, 스캔 범위의 완전성은 seqnum 연속성으로 확인합니다. journald는 기동할 때만 설정을 읽으므로 journald.conf와 drop-in 파일·디렉터리, 그리고 없는 경로의 가장 가까운 상위 디렉터리의 ctime이 현재 journald invocation 시작보다 늦으면 원래 rate limit을 확인할 수 없다고 보고 중단합니다. `/run/systemd` 아래 런타임 설정은 그 디렉터리가 계속 바뀌어 기동 뒤 삭제를 파일시각으로 배제할 수 없습니다. 그 경로의 journald 설정은 journald를 다시 시작하기 전에 지우지 마십시오.

이 복구는 systemd 257의 job 할당 규칙을 사용합니다. source 창부터 각 경계에서 manager 버전과 로그 설정, `NInstalledJobs`, 대기 job 목록을 읽고 저널을 동기화합니다. 버전을 확인할 수 없거나 창 사이에 바뀌면 중단합니다. 중앙 호스트의 Podman healthcheck transient service는 `LogLevelMax=notice`로 몇 초마다 job을 설치하면서 PID 1 기록을 남기지 않습니다. 그래서 경계 사이의 설치 수 증가는 PID 1 완료 기록 수보다 작지 않은지만 확인하고, 다른 unit의 기록 없는 설치는 허용합니다. 보호 unit(두 writer와 migrator)의 job은 PID 1 저널 기록과 unit 상태로 확인합니다. 이를 위해 각 경계에서 manager `LogLevel`이 `info`나 `debug`이고 `LogTarget`이 `journal`이나 `journal-or-kmsg`이며 보호 unit의 `LogLevelMax`가 없거나 `info` 이상이어야 하고, 아니면 중단합니다. 이 확인은 기록을 억제하는 설정을 거부할 뿐, journald가 밀려 PID 1의 기록 송신이 실패하는 경우까지 막지는 못합니다. 그런 job이 migrator를 실행했거나 writer를 바꿨다면 Podman 이벤트, InvocationID, 원장 대조가 잡습니다. 실행 전에 취소되어 저널에 없는 보호 unit job은 효과가 없으므로 따로 대조하지 않습니다. 두 daemon-reload와 각 schema·writer 요청 직전에는 정의와 두 writer를 다시 조회한 뒤 새 경계를 잡아 직전 경계부터의 저널을 검사합니다. 두 writer와 migrator의 대기 job이 없어야 합니다. supervisor는 writer 상태와 PID 1의 실행 hook·조건·drop-in을 대조한 뒤 마지막 저널 구간을 검사합니다. writer 기준은 봉인 소스와 승인 프로필로 별도 렌더링한 unit이며, 나머지 Quadlet drop-in 목록과 해시는 protected.after에서 가져옵니다. 승인 reload 전후의 UnitsLoadTimestampMonotonic과 저널 완료 수가 맞아야 합니다. schema와 writer 요청에는 의존성을 추가하지 않는 `EnqueueUnitJob`을 씁니다. 요청 직전에 기존 `init.scope`에 동작을 수행하지 않는 `nop` job 하나를 요청해 할당 번호를 기록하고, 응답 job 번호가 그 번호보다 커야 합니다. 기존 nop가 대기 중이거나 응답 번호가 nop 번호보다 작으면, 즉 nop보다 먼저 있던 job에 승인 요청이 병합되면 중단합니다. nop 요청과 승인 요청 사이의 짧은 간격에 다른 주체가 같은 보호 unit에 넣은 job과 병합되는 경우는 번호로 구분할 수 없습니다. 승인된 schema job이 실행되는 동안 같은 job에 병합된 후속 요청은 추가 실행을 만들지 않습니다. 수락하려면 승인된 invocation과 CID의 migrator start와 성공 종료가 각각 하나여야 합니다. conmon 성공 메시지는 invocation과 전체 CID로 연결하고 이미지 ID는 rootful Podman 시작 이벤트에서 확인합니다. 이벤트가 태그를 기록했다면 같은 CID·invocation의 Podman run 명령줄에 고정한 이미지 ID가 있어야 합니다. supervisor는 deploy 잠금을 coordinator와 공유하고 pidfd로 생존을 확인한 뒤 daemon-reload, schema 시작, writer 재시작과 할당 확인용 nop를 직접 보냅니다. 실제 발신 프로세스는 supervisor 종료 시 SIGKILL을 받습니다. 발신과 coordinator 종료가 겹치면 이미 접수됐을 수 있으므로 결과를 outcome_unknown으로 남기고 재발행하지 않습니다. 프로필 검사와 rollback --check의 StartTransientUnit은 worker-profile-pair.py가 systemd-run으로 요청합니다. 이 transient service는 보호 unit이 아니므로 다른 unit의 job과 같이 다룹니다. manager 요청·할당 증거는 해당 job 디렉터리에 남습니다. 게시기나 coordinator가 30분 안에 종료되지 않으면 supervisor가 로컬 자식 프로세스를 종료하고 접수된 manager job의 증거를 보존합니다.

복구 게시에서는 기존 `source.accepted`, API의 원 요청·종료 결과와 manager 기록, 두 writer의 invocation·CID·이미지·프로필, 로드된 migrator, 보호 bind를 대조합니다. 원본 증명과 각 창은 #841이 기록한 peer 10개(두 writer, holo-postgres, holo-pgbouncer, chatbotgo-bot·postgres·pgbouncer, twentyq-bot·postgres, 관리 대시보드 방화벽)의 invocation·CID·이미지가 그대로이고 `NRestarts=0`이며 컨테이너가 healthy여야 하고, 하나라도 다르면 중단합니다. source 창은 원본 증명을 통과한 뒤에 예약을 남기므로, 증명 단계의 거부는 창을 소모하지 않습니다. job 접수 여부가 불명확하거나 한쪽 writer가 이미 바뀌었으면 중단합니다. `current`나 이전 `active`를 되돌리지 않습니다. 새 전환 기록의 `installed`는 소스 선행 커밋이고 `running`과 `predecessor.writers`는 실행 선행 커밋과 두 writer의 실측 신원입니다.

이 경로는 `dv-tool-<SOURCE>`와 `dv-holo-{api,worker}-<SOURCE>` 원본 영수증이 남은 최초 전환용입니다. 해당 부팅의 manager 기록과 원래 요청 구간 전체의 저널 커서 연속성을 입증하지 못하면 사용할 수 없습니다. 후속 소스 게시는 정상 `--protected-source` 절차를 따릅니다. 게시기는 supervisor와 같은 `/opt` 잠금을 잡고, 세 창의 응답 해시와 전송 기록, 양쪽 job·롤백 파일 해시, worker stage와 transition 양쪽의 수신 원본에 결합된 `activation.confirmed`와 마지막 manager 커서를 요구합니다. 확인 파일 rename 후 디렉터리 동기화가 실패하면 `activation.confirmed`와 `activation.confirmation-not-accepted`가 공존할 수 있습니다. 실패 표시가 있으면 게시를 거부합니다. rename 후 프로세스나 연결이 끊겨 성공 표시만 남아도 kapu에서 receipt 창의 stdout과 종료 코드 0을 확보하지 못했다면 후속 게시를 승인하지 않습니다. 복구 종료 판정에는 보존된 영수증을 사용하며, 실행 중인 두 writer는 일반 봉인 선행 세대와 같은 기준으로 확인합니다. 그 사이의 재부팅이나 writer 재시작은 종료 판정을 바꾸지 않습니다.

소스 게시 후에도 두 앱은 이전 세대로 남습니다. 새 API 기동 창에서는 두 실행 세대가 모두 기록과 같아야 하고, worker 기동 창에서는 **수용된 새 API**와 **기록된 기존 worker**가 필요합니다. 각각 원 요청의 schema·writer job, manager invocation, CID, 실행 파일, readiness와 로드된 프로필로 수용합니다. 원장·보호 bind가 그대로인지 확인하며, 실패한 요청을 다시 발행하거나 자동으로 롤백하지 않습니다.

`app-sources/transition-OLD-NEW/`의 `previous.actual`은 검증한 이전 소스 바이트이며 `previous.metadata`는 그 원본의 신원·권한·시각입니다. 원본 inode는 그대로 남습니다. 같은 디렉터리에 두 보호 인프라 스냅샷과 Git 변경 목록을 보존합니다. `active`가 없거나 `incomplete OLD -> NEW`이면 후보를 활성화하지 않고 원래 시도의 영수증과 양쪽 세대를 보존합니다. 자동 재시도·stamp 수리·되돌리기는 없습니다. 정상 복구도 이전 소스·이미지/프로필·원장 호환성 및 실제 서비스 상태를 대조한 별도 승인 절차로만 합니다.

`active`가 유일한 게시 판정점입니다. 최초 store는 incomplete 레코드와 복구 영수증을 포함한 디렉터리 하나로 생기며, 후속 게시는 먼저 기존 `active`를 incomplete로 교체·동기화한 뒤 영수증을 공개합니다. 모든 검증과 영수증 동기화 뒤 새 SHA와 원자 교환합니다. 중앙의 mv가 GNU coreutils의 `--exchange --no-copy`를 지원하는지 incomplete 공개 전에 확인합니다. 기존 active를 incomplete로 바꾸는 첫 교환과 최종 교환에서 각각 열린 FD의 device·inode와 바이트를 밀려난 파일과 대조합니다. 복구 후 정상 게시도 마지막 교환 전에 두 writer의 이미지·건강·실행 프로필을 확인합니다. 그 교환이 보였더라도 응답·최종 동기화 확인을 잃으면 종료 상태 `75`와 `[OUTCOME] outcome_unknown`입니다. 이때 원래 lock·active·두 봉인 세대·해당 전환 영수증/출력의 `recovery` 경로를 대조하며, 같은 설치를 다시 실행하지 않습니다.

### 전체 설치 검사

설치 도구는 다음을 확인한 뒤에만 unit을 설치합니다.

- hostname이 `kapu`이면 `HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1` 없이 거부합니다(두 번째 dispatcher 방지).
- `compose.env`에 `HOLOLIVE_VALKEY_HA_ENABLED=1`과 비어 있지 않은 `VALKEY_SENTINEL_ADDRS`가 있어야 합니다.
- `HOLOLIVE_X_SPACES_ENABLED`·`HOLOLIVE_SCHEDULE_POSTER_ENABLED`·`HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED`·`HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED`는 `0` 또는 `1`이며, 그 값에 따라 각 선택 기능 drop-in을 설치하거나 지웁니다.
- `compose.env`의 PostgreSQL pool 값이 연결 예산(`scripts/deploy/lib/postgres-capacity.sh`)을 넘으면 거부합니다.
- 설치 전에 설치 뒤와 같은 구성(staged unit, 켜진 선택 기능 drop-in, 설치 도구가 건드리지 않는 기존 drop-in)이
  `quadlet -dryrun`을 통과해야 합니다. 실패하면 `/opt`·prestart·unit을 바꾸지 않습니다.
- 설치 뒤 실행 트리가 root 소유여야 합니다.

배포 트리에는 archive한 commit을 `.source-revision`으로 남깁니다. 배포 helper가 image revision을 이 값과 대조합니다.

방화벽 규칙(`scripts/systemd/admin-dashboard-ingress.nft`)이 바뀐 배포에서는 설치 뒤
`sudo systemctl reload admin-dashboard-ingress-firewall.service`로 적용합니다(아래 '기동·정지·의존').
이 규칙은 같은 호스트의 ChatBotGo(5432)·TwentyQ(5458) PostgreSQL 게시 포트도 loopback과 kapu(`bot_postgres_sources`)에서만 받습니다.
Tailscale은 tailscale0에서 전달하는 패킷을 masquerade하므로, 규칙이 없으면 PostgreSQL은 원격 접속을 모두 bridge gateway(`172.23.0.1`)로 봅니다.
`forward_preserve_postgres_source` 체인은 PostgreSQL 게시 `5433→5432`와 PgBouncer 게시 `6433→6432`의 `postgres_sources` 접속에만 그 표시를 지웁니다. 앱의 AP별 주소 판정은 PgBouncer frontend HBA가 맡고 PostgreSQL의 앱 접속은 고정 pooler 주소만 받습니다. exporter의 직접 접속과 두 봇 DB 게시·다른 게시 포트의 masquerade는 그대로 유지합니다.
두 봇의 DB unit(`chatbotgo-postgres`·`twentyq-postgres`)도 holo-postgres처럼 이 방화벽 unit을 `Requires=`하므로, 방화벽 unit이 시작에 실패하면 기동하지 않고 멈추면 함께 멈춥니다. reload는 두 DB에 영향을 주지 않습니다.

prestart helper는 `/usr/local/libexec/hololive-bot/quadlet-prestart`(+ `lib/`)로 설치됩니다.

## 호스트 값과 비밀

- 모든 unit이 `[Service] EnvironmentFile=/etc/stack-secrets/hololive-bot/compose.env`를 읽습니다.
  `[Service] Environment=`는 compose의 `${VAR:-기본값}` 기본값이며 compose.env가 우선합니다.
- compose.env는 `KEY=VALUE` 줄만 받습니다. 잘못된 key, 중복 key, 값의 앞뒤 공백은 prestart가 키 이름만 보고하고
  거부합니다. `KEY=` 빈 줄은 unit 기본값을 빈 값으로 덮으므로 기본값을 쓰려면 줄을 지웁니다.
- 비밀번호·API key는 `podman run` 인자에 남지 않도록 prestart가
  `/run/hololive-bot/quadlet-env/<container>.env`(디렉터리 0700, 파일 0600)에 써서 `EnvironmentFile=`로 넘깁니다.
  서비스별 env 파일(`bot.env`, `alarm-worker.env`, `youtube-collector.env`) 다음 순서라 compose의
  `environment` > `env_file` 우선순위를 유지합니다.
- PostgreSQL 로그인 비밀번호는 API `HOLOLIVE_DB_PASSWORD`, alarm-worker `HOLOLIVE_ALARM_WORKER_PASSWORD`,
  collector slot별 `HOLOLIVE_COLLECTOR_A_PASSWORD`~`D_PASSWORD`로 나누고 서로·`DB_PASSWORD`와 다르게 둡니다.
  이 창의 one-shot은 여섯 업무 역할의 로그인 비밀번호와 연결 한도를 변경하지 않습니다. 배포 이미지의 `db-migrate`는 창 전용 인자가 빠지면 DB 접속 전에 거부합니다. 신규 DB의 별도 초기화에서만 같은 이미지의 `--initialize`를 명시하며, 운영 one-shot은 `--schema-only`입니다.
  같은 비밀번호이면 기존 SCRAM verifier를 유지합니다. 실제 비밀번호를 바꿀 때는 해당 역할의 모든 앱 frontend를 먼저 drain·종료해야 하며, 비밀번호 변경 자체가 기존 세션을 폐기하지는 않습니다.
- compose에서 `:?`였던 필수 값(`VALKEY_SENTINEL_ADDRS`, `LIVE_LOGS_PATH`,
  `POSTGRES_PORT_BIND_IP`, `VALKEY_BIND_IP`, `HOLOLIVE_API_PORT_BIND_IP`, `HOLOLIVE_ADMIN_API_PORT_BIND_IP`,
  `HOLOLIVE_BOT_PORT_BIND_IP`)은 prestart가 확인합니다. tailnet publish 주소는 `tailscale0`에 붙을 때까지 90초 기다립니다.
- `hololive-api`의 `APP_VERSION`은 배포 트리 `hololive/hololive-api/VERSION`에서 읽습니다.

## Transaction pooling

이 절은 소스의 전환 계약입니다. 운영 적용 여부는 배포 baseline의 별도 수용 기록으로 판단합니다.

- 중앙 앱은 `holo-pgbouncer:6432`, AP a·b·d는 `100.100.1.8:6433`을 사용합니다. `compose.env`와 native host env의 host/port override도 함께 바꿉니다.
- `holo-pgbouncer`의 bridge 주소는 `172.23.255.254`입니다. PostgreSQL은 이 `/32`에서 오는 여섯 앱 역할의 SCRAM과 `hololive_pgbouncer_auth`의 인증서 로그인만 받습니다. 앱 비밀번호·verifier를 pooler 파일에 복제하거나 backend 공용 user로 강제하지 않습니다.
- `hololive_pooler_auth.lookup(text)`는 앱 DB의 관리자 소유 함수입니다. cert-only auth 역할의 연결 한도는 1이며, 현재 SCRAM verifier만 조회합니다. 함수는 LOGIN·만료·특권을 검사하고 HBA는 DB·역할·source·인증서를 제한합니다. 기존 인증 객체의 잘못된 owner/ACL·예상 밖 객체는 자동 수정하지 않고 bootstrap을 거부합니다.
- 관리·migration·nightly Unix socket과 exporter의 직접 `5433` 경로는 유지합니다. migrator의 session advisory lock은 pooler를 통과하지 않습니다.
- 공통 native 이미지는 `localhost/iris-stack-pgbouncer:1.26.1-guc1`이며 기존 native 이미지와 같이 `zlib=1.4-r0`을 고정합니다. 정식 1.26.1에서 재현한 미보고 startup GUC 오염을 [upstream commit `22d0aa1`](https://github.com/pgbouncer/pgbouncer/commit/22d0aa1ce20cf3a29eb8ed267caa5570a76a812f)으로 수정했습니다. 이 수정이 포함된 정식 버전으로 전환할 때 patch를 제거하고 통합 검사를 유지합니다. ChatBotGo는 같은 바이너리 이미지만 재사용하며 인스턴스·인증서·DB·예산은 별개입니다.
- `pool_mode=transaction`, `max_prepared_statements=200`, pgx `cache_statement`를 유지합니다. `track_extra_parameters=statement_timeout`로 startup의 30초 제한을 보존합니다. 임의 session `SET`, SQL-level `PREPARE`, session advisory lock을 transaction pooling 지원 기능으로 간주하지 않습니다.
- backend 대기 `query_wait_timeout=5`는 pgxdb의 local acquire 5초와 별도입니다. backend idle은 600초이며 통합 시험을 이유로 운영 값을 1초로 줄이지 않습니다.
- `scripts/deploy/lib/postgres-capacity.sh`가 compose.env·설치 unit·drop-in의 effective 값으로 비밀 없는 `pgbouncer.ini`를 생성합니다. 역할별 backend 상한은 해당 앱 풀 최대값의 합이고 reserve pool은 0입니다. 기본값은 앱46+auth1, 정비 포함52/일반58로 여유6입니다. admin pool max3이면 앱45+auth1, 전체51로 여유7입니다. 앱 활성화·재기동·복구에서는 실행 pooler가 mount한 설정과 대조합니다. 풀 크기만 바꾸고 앱만 재배포하면 사전에 거부되므로, 용량 변경은 writer를 보존·정지한 뒤 pooler 설정 재생성과 재기동까지 같은 창에서 처리합니다.
- 예약 control 진행 보장은 정상 단일 배포와 선언된 풀 상한 안에서의 계약입니다. 중복 writer나 임의 추가 frontend까지 무기아를 보장하지 않습니다.

### Epoch 4 배포 경계

후속 변경이 없는 20.0.0 one-shot은 기존 032 head나 새 기준선 1개를 **읽기만** 한다. 정상 032 DB에서는 DDL·업무 DML·원장·로그인·pooler 변경이 없다. 원래 PostgreSQL/PgBouncer 세대·보호 inode·서비스와 writer endpoint를 그대로 보존한다. 19.2.1 source-only 창은 migration/보호 경로 변경을 거부하는 것이 정상이다. 20.0.0 전체 소스 staging, peer CAS, worker/collector별 이미지·프로필·실제 pooled SQL 수용은 별도 릴리스 창에서 증명한다. 새 DB 초기화/구 DB 복구는 [규약](../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md)을 따른다.

### 과거 030 카드 전환 (19.2 릴리스 전용)

다음 절차는 19.2의 **해당 checkout·도구**로만 수행한다. 20.0.0으로 부분 030/031을 재개하거나 미확인 hold를 해제하지 않는다.

이 카드 릴리스로 direct→pooled 전환을 시작하지 않습니다. 먼저 `release/hololive-pool-only-030`의 절차로 여섯 역할의 pooled SQL과 변경되지 않은 원장 030을 수용합니다. 그 작업의 hold를 재사용하지 않습니다. 아래는 **이미 풀링이 운영 중인 별도 카드 창**입니다.

1. 운영 승인 전 같은 소스의 API·worker·b/c/d collector/issuer·native a 번들과 현재 상태의 복구 방법을 확보합니다. direct용 API 단독 이미지는 사용하지 않습니다. `holo-postgres`, `holo-pgbouncer`의 CID·image ID·`StartedAt`, HBA·풀러 설정의 실제 mount와 endpoint를 기록합니다. 이 창에 인프라 이미지·인증서·방화벽·접속지 변경이나 풀러 재시작은 없습니다.
2. `sync-opt-current.sh --application-only`로 중앙의 실행 트리와 카드 migrator 정의만 설치합니다. 이전 풀러 image pin을 유지합니다. kapu에서 `po-central-cutover.sh c quiesce-verified "$PAIR_DIR" "$TRANSPORT_SHA256"`를 실행하고 b도 같은 방식으로 새 hold에 정지합니다. a는 native `--quiesce W2`, d는 [봉인된 카드 d 전용 tar](../podman/kapu/README.md)의 `kapu-d-cutover.py quiesce --already-pooled <새 hold> <W2 전체 SHA> <collector image ID> <issuer image ID> <collector unit SHA256> <issuer unit SHA256>`로 정지합니다. 중앙은 설치한 `quadlet-redeploy-service.sh --quiesce <API ID> <worker ID>`로 정지합니다. `--quiesce-before-sync`, 일반 단일 배포, 과거 실패 영수증으로 대체하지 않습니다.
3. 여섯 writer의 원래 세대 정상 종료·영속 기동 차단, 풀러의 업무 frontend 부재, 실제 업무 backend와 준비된 트랜잭션 0을 확인합니다. idle backend가 남으면 `server_idle_timeout=600`에 따라 자연 종료될 때까지 기다립니다. 정지 증거를 만들기 위한 세션 강제 종료나 풀러 재시작은 금지합니다. 이 시점으로 실제 복원 가능한 DB 백업·유실 범위를 별도로 검증·승인하지 않았으면 031을 실행하지 않습니다.
4. `quadlet-redeploy-service.sh --resume <이번 API release> <이번 worker release>`의 private-socket one-shot이 **migrator 자격만** 받아 `--schema-only`로 031→032를 적용합니다. `card-migration-guard.py`가 관리자 읽기 전용 소켓 세션에서 신규 업무 인증을 차단하고, 여섯 역할의 차단과 PgBouncer `SHOW SOCKETS`의 업무·로그인 중 frontend 부재를 확인합니다. 원장 030에서는 이 실세션이 없으면 migrator가 거부합니다. DDL 중에는 migrator 세션도 인증 차단을 유지합니다. 풀러의 PAUSE/DISABLE·강제 종료·설정 변경은 없습니다. 원장·checksum·무효 인덱스·부분 적용 흔적을 먼저 검사합니다. 031은 배치별 커밋이므로 실패 시 이미지 되돌리기나 자동 재시도가 아닙니다. hold를 유지하고 실제 영향에 따른 복구 또는 명시적 fix-forward를 새로 승인받습니다.
5. worker→API→c→b→a(`--resume W2`)→d(`resume --already-pooled`)에서 각 후보의 실제 pooled SQL·준비 상태·프로필과 handoff를 확인합니다. 031 이후 구 writer를 복원하지 않습니다. 중앙은 마지막 API의 실행 검증 후에도 같은 PG·풀러 실행 세대를 재확인하고 나서만 hold를 해제합니다. 이전 API/worker에 `text_file` outbox 계약이 없으면 같은 endpoint여도 rollback을 거부합니다. **원장과 checksum 032, 변경 전과 같은 PostgreSQL·풀러 실행 세대/경로, 멤버별 카드 GET**이 수용 기준입니다. 이후에만 Console gateway와 대응 APK를 순차 수용합니다. 운영 PATCH·원격 업데이트 게시·실기기 설치·Git 게시 권한은 별개입니다.

중앙 API·worker와 b/c/d의 Quadlet은 **기동 차단 → 원래 unit으로 정지 → 최종 mask** 순서입니다. 활성 unit에 `/dev/null` mask를 먼저 로드하면 원래 `ExecStop`과 `KillMode`가 사라집니다. 반대로 mask의 reload만 미루면 기존 `Wants=`가 writer를 다시 기동할 수 있습니다.

같은 변경의 `systemd-stop-guard.py`가 각 unit에 설치하는 `90-hololive-stop-guard.conf`는 두 경로를 차단합니다.

- 명시적 기동·의존성·재부팅: 작업 소유 anchor가 있으면 `ConditionPathExists=!…`가 성립하지 않습니다.
- 프로세스 자체 종료: systemd의 자동 재시작은 위 조건을 다시 검사하지 않으므로 `Restart=no`, 빈 `RestartForceExitStatus=`가 실제 manager에 적용돼야 합니다.

어느 설정도 이미 실행 중인 프로세스를 정지시키지는 않습니다. 원래 정지 명령과 정책이 그대로인 상태에서 manager가 원 invocation을 정지했는지, 같은 시간 범위에 원래 Podman CID의 종료 이벤트가 단 한 건인지 확인합니다. 기동 차단 파일이나 컨테이너 부재만으로 정상 종료를 인정하지 않습니다. 부분 실패 시 guard·anchor·복구점·journal을 보존하고 임의 삭제, `reset-failed`, DB 변경, 다음 writer 기동을 하지 않습니다.
재개할 때 중앙과 b/c는 manager의 원래 start job·invocation 및 Podman `State.ConmonPid`와 unit `MainPID`의 일치를 확인합니다. 기동 클라이언트가 실패해도 요청을 반복하지 않습니다. 같은 job이 남았다면 가드 아래에서 해당 unit의 `TimeoutStartUSec` 범위로만 관찰합니다. 확인된 원래 writer는 정지하고, 교체된 컨테이너를 대신 정지하거나 후속 writer를 열지 않습니다.

`b/c quiesce`는 자기 mask의 inode와 두 컨테이너의 부재를 함께 확인합니다. `po-broker`는 의도적으로 SIGTERM을 다시 보내므로 Podman이 143, systemd가 `failed`를 기록할 수 있습니다. 성공한 `stop` 직전에 실행 중이던 바로 그 issuer invocation이고 두 PID가 0인 경우에만 이 상태를 차단으로 인정합니다. 실패 이력을 지우거나 collector의 오류를 정상 종료로 취급하지 않습니다. 종료 영수증이 없는 기존 hold에 영수증을 소급 작성하지 않습니다. `[OUTCOME] unconfirmed`이면 DB 변경·추가 기동을 중단하고 mask·원래 journal·복구점을 보존합니다.
정지 journal 조회가 실패하면 `[hold] stop journal unconfirmed`의 `unit`, `invocation`, `since`(epoch 마이크로초), `journal_status`를 보존합니다. 상태 0인데도 이 진단이 나오면 해당 범위에 일치한 이벤트가 없었다는 뜻입니다. 나중에 같은 이벤트를 찾더라도 원래 종료 결과나 영수증을 바꾸지 않습니다.

### 복구와 자격 증명 변경

- 중앙·b·native rollback은 현재 env/drop-in까지 반영한 endpoint를 이전 복구점과 비교합니다. 다르거나 해석되지 않으면 재기동 전에 거부합니다. 이전 HBA/DB 설정·effective endpoint를 별도 승인 아래 정합화한 뒤 기존 `--check`/`--apply`를 다시 실행합니다. native recovery guard도 기존 복구 절차로 해제하며 우회 flag는 없습니다.
- 실제 비밀번호 회전은 그 역할의 frontend를 먼저 종료하고 bootstrap한 뒤 새 비밀번호로 재접속합니다. 다른 역할은 verifier를 유지하므로 정상 재배포가 fleet 전체의 SCRAM 키를 무효화하지 않습니다. 만료/NOLOGIN도 새 인증 거부와 기존 세션 종료를 구분합니다.
- 로컬 `PGBOUNCER_TEST_IMAGE=<host-arch image> bash scripts/deploy/test-pgbouncer.sh`는 양방향 TLS, 역할/source 거부, pgx prepared backend 교체·동명 동시 사용·재연결, timeout 격리, 예약 control, 강제 backend 종료의 오류/rollback/후속 연결, 동일 비밀번호 재bootstrap과 실제 회전을 검사합니다. native fixture의 PAUSE/RESUME·강제 종료는 임시 컨테이너에만 적용합니다.

## 기동·정지·의존

- 상시 unit은 `WantedBy=multi-user.target`이며 `Notify=healthy`라 start job은 healthy가 된 뒤 끝납니다.
- `HealthOnFailure=stop` + `Restart=always`가 자동 복구입니다. unhealthy container는 StopSignal(PostgreSQL `SIGINT`,
  ingress `SIGQUIT`, 나머지 `SIGTERM`)과 `StopTimeout`(api·worker 45초, PostgreSQL 120초)을 지켜 멈춘 뒤 systemd가 다시
  띄웁니다. `kill`(SIGKILL)은 배치 전송과 checkpoint를 끊으므로 쓰지 않습니다. collector c와 PO issuer는 예외(unhealthy만으로 멈추지 않음)입니다.
- `holo-postgres`는 `TimeoutStopSec=150`입니다. `ExecStop`(`podman rm`)이 `StopTimeout=120`을 다 쓰기 전에 systemd
  기본 90초가 끊지 않게 합니다.
- `Tmpfs=`에는 `noexec,nosuid,nodev`를 적습니다. Podman은 옵션을 준 `--tmpfs`에 Docker처럼 이 옵션을 붙이지 않습니다.
- 런타임 peer(PostgreSQL·Valkey·worker)는 `Wants=`+`After=`입니다. `Requires=`는 명시적 stop/restart를
  소비자에게 전파해 primary fence(소비자 유지)와 Sentinel HA를 깨뜨리기 때문입니다.
  보안 전제인 `admin-dashboard-ingress-firewall.service`와 migration의 PostgreSQL 의존만 `Requires=`입니다.
- 방화벽 규칙 갱신은 `sudo systemctl reload admin-dashboard-ingress-firewall.service`로만 합니다. reload는 table
  선언·삭제·재정의를 한 nft transaction으로 적용해 규칙이 빈 순간이 없고, 새 파일이 거부되면 기존 table이 남습니다.
  `restart`·`stop`은 `Requires=`로 묶인 `holo-postgres`·`hololive-api`·`admin-dashboard-ingress`와 두 봇의
  `chatbotgo-postgres`·`twentyq-postgres`를 함께 재시작·정지합니다. reload는 전파되지 않습니다(systemd 257 user unit으로 실측).
- `stop` 뒤 방화벽 unit을 다시 `start`해도 함께 멈춘 unit은 다시 뜨지 않습니다. 방화벽 unit이 active인지 확인한 뒤
  `sudo systemctl start holo-postgres.service hololive-api.service admin-dashboard-ingress.service chatbotgo-postgres.service twentyq-postgres.service`로
  직접 기동하고 각 unit 상태를 확인합니다.
- 방화벽 unit을 없애려면 먼저 이 저장소와 두 봇 저장소 unit의 `Requires=`·`After=` 의존을 지우고 반영합니다.
  의존이 남은 채 unit만 지우면 다음 기동이 실패합니다.
- `hololive-api`를 시작하면 `hololive-alarm-worker`도 함께 시작됩니다(Wants).
- collector c는 compose.env의 `HOLOLIVE_DISABLE_YOUTUBE_COLLECTOR=1`이면 `ExecCondition`으로 건너뜁니다.

```bash
sudo systemctl status hololive-api.service
sudo systemctl show hololive-api.service -p NRestarts   # 마지막 명시적 restart 뒤 자동 재시작 횟수
sudo journalctl -u hololive-api.service --since -15m
```

API·worker의 image 교체와 migration은 아래 '배포 helper'로만 합니다.

## Primary fence

모든 unit은 `/var/lib/hololive-postgres-fence/fence.intent`·`fenced` 중 하나라도 있으면 시작하지 않습니다
(`ConditionPathExists=!`). `quadlet-redeploy-service.sh`와 `quadlet-rollback-service.sh`도 표식이 있으면 진행을
거부합니다. 표식을 쓰고 치우던 failover fence·unfence helper는 저장소에서 지웠으므로(커밋
`bb000290e`에 남아 있는 git 이력) 지금은 표식을 만드는 운영 경로가 없습니다. 표식이 보이면 직접 지우지 말고 원인을
확인한 뒤 별도 승인으로 처리합니다.

표식 없이 `systemctl stop holo-postgres.service`만 하면, `Wants=`로 묶인 소비자(`hololive-api`, `hololive-alarm-worker`, `hololive-youtube-collector-c`)가 crash나
`HealthOnFailure=stop`으로 자동 재시작할 때 `holo-postgres` start job도 함께 걸려 DB가 다시 기동됩니다.
표식 조건이 막는 것은 명시적 start와 이런 의존 start job입니다(`skipped because of an unmet condition check`).
`Restart=always`의 자동 재시작은 `ConditionPathExists=`를 다시 평가하지 않으므로, 표식이 있어도 fence 중 죽은
소비자는 다시 뜹니다(systemd 257 rootless Quadlet으로 실측). 표식 없이 DB를 멈춰야 하는 정비라면 소비자도 함께 멈춥니다.

## worker profile drop-in과 배포 helper

release별 image·worker profile 고정은 `scripts/deploy/lib/worker-profile-pair.py`가 소유하는
`/etc/containers/systemd/hololive-bot/<unit>.container.d/50-worker-profile.conf` drop-in입니다. 설치 도구는 이 파일을
건드리지 않습니다. drop-in은 `Image=sha256:<id>`, pair label, release 디렉터리의 읽기 전용 mount와
`STACK_WORKER_PROFILE_FILE`을 바꾸고, 기본 unit의 원본 mirror mount는 그대로 둡니다. 서비스 이름은 `hololive-api`,
`hololive-alarm-worker`, `youtube-collector`(unit `hololive-youtube-collector-c`)이며 이 서비스들은 `podman`으로 고정됩니다.

### 배포 helper

API·worker image 교체는 배포 트리 사본의 `scripts/deploy/quadlet-redeploy-service.sh`로 root로 실행합니다.
다른 위치의 사본은 거부합니다. collector c(와 iris-seoul b)는 `po-central-cutover.sh <c|b>`가 소유합니다.

```bash
helper=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh
sudo "$helper" hololive-api sha256:<api-id>
# worker의 migration은 같은 revision의 API image로 실행한다(db-migrate는 API image에만 있다)
sudo "$helper" hololive-alarm-worker sha256:<worker-id> sha256:<api-id>
```

image 인자는 build host의 identity(`sha256:` + `podman image inspect --format '{{.Id}}'`)나 적재한 tag입니다.
helper는 다음 순서로 진행하며 어느 단계도 재시도하지 않습니다.

1. preflight: 배포 트리 revision(`sync-opt-current.sh`가 남긴 `.source-revision`)과 image의
   `org.opencontainers.image.revision`·title·`linux/<host arch>`, primary fence 표식 부재, unit 실행 중,
   host `compose.env` 기준 PostgreSQL 연결 예산(`postgres_capacity_assert_target`)을 확인합니다.
2. 실행 중 container의 이전 pair를 `artifacts/worker-profile/releases/<service>-<UTC>-<suffix>/previous`에 보존합니다
   (`capture`. drop-in이 없는 첫 도입은 `prepare`가 실행 중 image를 먼저 고정합니다). rollback drop-in을 만들고 이전
   image의 `--check-worker-profile`을 확인합니다.
3. candidate pair, `--check-worker-profile`, `verify-source`를 확인합니다.
4. 활성화 전에 `hololive-db-migrate.container.d/50-migrator-image.conf`(`Image=sha256:<API id>`)로 migrator를 고정하고
   `systemctl start hololive-db-migrate.service`를 실행합니다. fence 조건으로 건너뛴 oneshot도 start는 0으로 끝나므로 start
   전후 모두 fence 표식이 없어야 합니다. 실패하면 활성화하지 않고 `[OUTCOME] not_activated`로 끝납니다.
5. candidate drop-in을 활성화하고 `daemon-reload`, `NRestarts` 기준 기록, `systemctl restart`(`Notify=healthy`가
   health gate) 뒤 `verify-live`와 `NRestarts=0`(명시적 restart가 0으로 되돌림)을 확인합니다.
6. 활성화 뒤 실패하면 자동으로 되돌리지 않고 `[OUTCOME] failed_after_activation`으로
   끝납니다. candidate pair와 release 디렉터리는 그대로 둡니다. 이번 배포가 migration을 적용했을 수 있으므로, 이전 pair 복원은
   스키마·admission 호환성 승인 뒤 `scripts/deploy/quadlet-rollback-service.sh <release> --check`와 `--apply`로 합니다.
   migration과 schema는 어느 경우에도 되돌리지 않습니다.

`previous-pair`에서는 서로 다른 두 가지를 확인합니다.

- `capture`·`prepare`는 **실행 중인 writer**의 불변 ID로 로드된 프로필 해시를 확인합니다. 이전 이미지로 네트워크 없는 읽기 전용 컨테이너를 만들고, `podman init` 뒤 실제 UID/GID 매핑·보조 그룹·SELinux MCS·환경과 bind inode를 대조한 후에만 `--check-worker-profile`을 실행합니다. 프로필만 이번에 보관할 바이트로 바꿉니다. 환경값은 호스트 명령행이나 env 파일에 기록하지 않습니다.
- 이어지는 `check`는 **보존한 기본 unit으로도** 그 이미지와 프로필이 시작 가능한지 검사합니다. 이 검사가 첫 검사를 대체하지 않습니다. 원 writer의 정지·재시작·이름 교체, 바뀐 bind, 접근 거부는 새 pair로 게시하지 않습니다. 정지된 writer를 검사 목적으로 켜지 말고 이미 보존한 release로만 재개합니다.

kapu의 SELinux Enforcing 실환경 회귀: `sudo python3 -I -B scripts/deploy/lib/worker-profile-capture_podman_test.py <로컬-host-arch-Alpine-이미지>`. rootful Podman에 이미 있는 이미지를 사용하며 운영 설정·비밀·서비스는 사용하지 않습니다.

### Iris 소비자 동시 전환

인증 계약처럼 API·alarm-worker와 외부 Iris를 같은 창에서 바꿔야 하면 두 소비자를 먼저 보존·정지합니다.
단일 서비스 helper를 정지된 unit에 억지로 적용하거나 API의 `Wants`가 구형 worker를 기동하게 두지 않습니다.

```bash
helper=/opt/hololive-bot/compose/current/scripts/deploy/quadlet-redeploy-service.sh
sudo "$helper" --quiesce sha256:<api-id> sha256:<worker-id>
# quiesced 결과의 두 release 경로를 보존하고, 승인된 다른 소비자·Iris 전환을 완료한다.
sudo "$helper" --resume '<api-release-dir>' '<alarm-release-dir>'
```

- `--quiesce`는 실행 중 이전 pair와 후보를 모두 검증·보존한 뒤 두 unit을 함께 정지하고 영속 mask합니다.
  호스트 재부팅 뒤에도 명시적 재개 전에는 구형 writer를 기동하지 않습니다. `[OUTCOME] quiesced`는 writer
  정지만 증명하며 migration·후보 활성화·배포 완료를 뜻하지 않습니다.
- 중앙 두 writer의 소유 표식 `artifacts/worker-profile/releases/.central-writer-hold`는 해당 API release의 `captured.invocation`과 같은 inode입니다. 부분 정지라도 이 표식이나 기동 차단 파일이 남아 있으면 단일 배포와 단일 `rollback --apply`는 거부합니다. 같은 내용을 복사하거나 표식을 지워 재개하지 않습니다.
- `--resume`은 두 release가 같은 정지 작업에서 나왔고 그 작업이 만든 영속 mask가 유지되는지 확인합니다. 설치된 후보의 revision·image·프로필·기본 unit·실효 endpoint가 일치해야 합니다. 두 후보 drop-in을 게시하더라도 API는 worker가 수용될 때까지 차단합니다.
- 완료는 exit 0과 두 서비스의 `[OUTCOME] deployed`, 실제 image/profile·health·`NRestarts=0`으로 확인합니다.
  실행 중 pair를 잃은 뒤 과거 파일 이름만으로 복구점을 만들지 않습니다.
- 준비 뒤 drift·부분 게시·실패는 성공으로 바꾸지 않습니다. 기동 전 실패는 hold를 유지하며, 부분 mask나
  기동 후 실패는 실제 unit 상태를 먼저 확인합니다. 자동 unmask·rollback은 없습니다. 원인을 해결한 명시적
  재개도 두 unit의 hold와 같은 보존 pair를 요구하며, 다른 pair나 배포 revision은 거부합니다.


### 컷오버 직후 첫 고정

컷오버로 처음 뜬 app container는 drop-in이 없어 `Image=${..._IMAGE}`(compose.env 값이나 `:prod` tag)를 따릅니다.
그대로 두면 tag를 옮긴 적재 뒤의 crash·unhealthy 재시작이 migration 전에 새 image로 뜨고, 되돌릴 이전 pair도 남지
않습니다. 세 app unit이 healthy가 된 직후 실행 중 container 기준으로 `prepare`해 첫 drop-in을 만듭니다. 실행 중인
image를 그대로 고정하므로 재시작하지 않습니다.

```bash
root=/opt/hololive-bot/compose/current
pair="$root/scripts/deploy/lib/worker-profile-pair.py"
release="$root/artifacts/worker-profile/releases/cutover-$(date -u +%Y%m%dT%H%M%SZ)"
sudo python3 "$pair" prepare hololive-api "$release/hololive-api/previous" hololive-api "$root"
sudo python3 "$pair" prepare hololive-alarm-worker "$release/hololive-alarm-worker/previous" hololive-alarm-worker "$root"
sudo python3 "$pair" prepare youtube-collector "$release/youtube-collector/previous" hololive-youtube-collector-c "$root"
sudo systemctl daemon-reload
```

### 개별 단계

helper와 `po-central-remote.sh`가 쓰는 pair 도구 단계입니다. 복구·검사에서 직접 쓸 때 참고합니다.

```bash
root=/opt/hololive-bot/compose/current
pair="$root/artifacts/worker-profile/releases/<release>/<service>/candidate"
# 후보: 기본 unit을 stdin으로, 배포할 image identity를 인자로 준다
sudo python3 scripts/deploy/lib/worker-profile-pair.py candidate <service> "$pair" sha256:<id> \
  < deploy/quadlet/hololive-bot/<unit>.container
sudo python3 scripts/deploy/lib/worker-profile-pair.py verify-source <service> "$pair"
# 활성화: drop-in 설치 + active pointer. 재시작은 승인된 절차에서 한다
sudo python3 scripts/deploy/lib/worker-profile-pair.py activate <service> "$pair" "$root"
sudo systemctl daemon-reload && sudo systemctl restart <unit>.service
sudo python3 scripts/deploy/lib/worker-profile-pair.py verify-live <service> "$pair" <container>
```

`prepare`·`capture`(실행 중인 이전 프로세스의 profile 고정)와 `rollback <service> <pair> [output]`도 같은 drop-in을
만듭니다. `hololive-api` rollback drop-in은 이전 image의 `APP_VERSION`을 `Environment=`로 고정합니다(Podman은
`--env`를 `--env-file`보다 우선합니다).

## 이미지

unit은 `Image=${..._IMAGE}`와 `Pull=never`이고 `[Service]` 기본값은 `localhost/<name>:<tag>`입니다. 실제 identity
(`sha256:` + `podman image inspect --format '{{.Id}}'`)는 image마다 다음이 소유합니다. 빌드는
`./build-all.sh --build-only --no-bump`(`podman build`, 다른 아키텍처는 `HOLOLIVE_BUILD_PLATFORM=linux/arm64`와 등록된
binfmt가 필요)입니다.

| image | identity를 정하는 곳 |
| --- | --- |
| `hololive-api`, `hololive-alarm-worker`, collector c | `50-worker-profile.conf`의 `Image=sha256:<id>`. compose.env의 `HOLOLIVE_*_IMAGE`와 `:prod`는 drop-in이 없을 때(컷오버 직후 첫 고정 전)만 쓰입니다 |
| `hololive-db-migrate` | 배포 helper가 쓰는 `50-migrator-image.conf`. 수동 `systemctl start hololive-db-migrate.service`도 마지막 배포의 migrator를 씁니다 |
| `POSTGRES_IMAGE`, `VALKEY_IMAGE`, `NGINX_IMAGE` | host `compose.env`의 Podman identity(`sha256:<id>`) |
| PO issuer(`hololive-youtube-po-c`) | unit 기본 `localhost/hololive-youtube-po-sandbox:prod` tag |

- infra image(`POSTGRES_IMAGE`, `VALKEY_IMAGE`, `NGINX_IMAGE`)는 compose.env에 `sha256:` + `podman image inspect --format '{{.Id}}'`로
  둡니다. `name:tag@sha256:<digest>` 형식은 Podman store에서 찾지 못해 `Pull=never` unit이 5초 간격 재시작만
  반복합니다. 값은 stack-secrets master(`hosts/hololive-seoul/hololive-bot/compose.env`)에 반영해 `sync-host.sh` 미러가
  되돌리지 않게 합니다. 교체는 load → build host identity 대조 → compose.env 갱신 → 승인된 restart 순서입니다.
- PO issuer는 `:prod` tag를 따르며 `:prod`는 `scripts/deploy/lib/po-central-remote.sh`만 옮깁니다(`:candidate`로 적재해
  검증한 뒤 cutover에서 `:prod`로 옮기고, 실패하면 `:rollback-*`에서 되돌립니다). compose.env에
  `HOLOLIVE_YOUTUBE_PO_IMAGE`를 두지 않습니다. 빈 값도 unit 기본값을 덮어 기동이 실패하고, sha256으로 고정하면 이
  스크립트의 tag 기반 교체·복원과 어긋납니다.
