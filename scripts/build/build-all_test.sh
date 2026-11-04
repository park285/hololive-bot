#!/usr/bin/env bash
# build-all.sh를 가짜 podman으로 실행한다. 배포 절차는 image revision label을 출처로 대조하므로
# clean source revision이 build-arg로 들어가고 label로 확인되는지, collector가 PO issuer와 같은 revision으로
# 짝지어 빌드되는지, dirty worktree가 검증된 revision을 주장하지 않는지 확인한다.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fail() { echo "[FAIL] $*" >&2; exit 1; }
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
repo="$tmp/repo"
fakebin="$tmp/bin"
export FAKE_PODMAN_LOG="$tmp/podman.log" FAKE_PODMAN_STATE="$tmp/images"
mkdir -p "$repo/scripts/deploy/lib" "$repo/hololive/hololive-api" "$repo/hololive/hololive-alarm-worker" \
  "$fakebin" "$FAKE_PODMAN_STATE"
cp "$ROOT_DIR/build-all.sh" "$repo/build-all.sh"
cp "$ROOT_DIR/scripts/deploy/lib/compose-services.sh" "$ROOT_DIR/scripts/deploy/lib/source-revision.sh" \
  "$repo/scripts/deploy/lib/"
printf '1.2.3\n' > "$repo/hololive/hololive-api/VERSION"
printf '1.2.4\n' > "$repo/hololive/hololive-alarm-worker/VERSION"
git -C "$repo" init -q
git -C "$repo" add -A
git -C "$repo" -c user.name=fixture -c user.email=fixture@example.invalid commit -qm fixture
head="$(git -C "$repo" rev-parse HEAD)"

# build는 --build-arg REVISION 값(또는 FAKE_LABEL)을 tag의 label로 저장하고 image inspect가 돌려준다.
cat > "$fakebin/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_PODMAN_LOG"
case "$1" in
  build)
    tag='' revision=''
    while (($#)); do
      case "$1" in
        --tag) tag="$2"; shift ;;
        --build-arg) [[ "$2" != REVISION=* ]] || revision="${2#REVISION=}"; shift ;;
      esac
      shift
    done
    printf '%s\n' "${FAKE_LABEL:-$revision}" > "$FAKE_PODMAN_STATE/${tag//[\/:]/_}"
    ;;
  image)
    [[ "$2" == inspect ]] || exit 2
    ref="${*: -1}"
    cat "$FAKE_PODMAN_STATE/${ref//[\/:]/_}"
    ;;
  info) printf 'amd64\n' ;;
  *) exit 2 ;;
esac
EOF
chmod +x "$fakebin/podman"

run_build() {
  : > "$FAKE_PODMAN_LOG"
  (cd "$repo" && PATH="$fakebin:$PATH" ./build-all.sh --no-bump --skip-local-ci "$@") > "$tmp/out" 2>&1
}
builds() { grep '^build ' "$FAKE_PODMAN_LOG" || true; }

run_build youtube-collector || { cat "$tmp/out" >&2; fail 'clean collector build failed'; }
[[ "$(builds | wc -l)" == 2 ]] || fail 'collector build must pair the PO issuer'
for image in localhost/hololive-youtube-collector:prod localhost/hololive-youtube-po-sandbox:prod; do
  line="$(builds | grep -F -- "--tag $image ")" || fail "missing build for $image"
  [[ "$line" == *"--format docker "* ]] || fail "$image must keep Docker image metadata for the Seoul AP"
  [[ "$line" == *"--build-arg REVISION=$head "* && "$line" == *"--build-arg VERSION=1.2.3 "* ]] \
    || fail "$image did not receive the clean source revision and release version"
  grep -qx "image inspect --format {{index .Config.Labels \"org.opencontainers.image.revision\"}} $image" "$FAKE_PODMAN_LOG" \
    || fail "$image revision label was not verified"
done
echo '[PASS] clean collector build pairs the PO issuer and verifies both revision labels'

if FAKE_LABEL="$(printf '%040d' 0)" run_build hololive-alarm-worker; then
  fail 'revision label mismatch must fail the build'
fi
grep -q 'revision mismatch' "$tmp/out" || fail 'label mismatch was not reported'
builds | grep -q -- '--build-arg VERSION=1.2.4 ' || fail 'alarm-worker must build with its own VERSION'
echo '[PASS] image whose revision label differs from the source is rejected'

touch "$repo/untracked"
run_build hololive-api || { cat "$tmp/out" >&2; fail 'dirty build failed'; }
builds | grep -q -- '--build-arg REVISION=unknown ' || fail 'dirty worktree must not claim a source revision'
! grep -q '^image inspect' "$FAKE_PODMAN_LOG" || fail 'dirty build must not verify an unknown revision'
echo '[PASS] dirty worktree builds with REVISION=unknown'

if run_build; then
  fail 'build-all without targets must refuse'
fi
[[ ! -s "$FAKE_PODMAN_LOG" ]] || fail 'refused build-all invoked podman'
echo '[PASS] build-all without targets refuses before building'
