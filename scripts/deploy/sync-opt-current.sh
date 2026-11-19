#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
OPT_ROOT="${OPT_ROOT:-/opt/hololive-bot/compose}"
OPT_CURRENT="${OPT_ROOT}/current"
QUADLET_DIR=/etc/containers/systemd/hololive-bot
LIBEXEC_DIR=/usr/local/libexec/hololive-bot
COMPOSE_ENV=/etc/stack-secrets/hololive-bot/compose.env

log() { printf '[sync-opt-current] %s\n' "$*"; }
die() {
  if [[ -n "${diagnostic_fd:-}" ]]; then
    publication_diagnostic "[sync-opt-current] ERROR: $*"
  else
    printf '[sync-opt-current] ERROR: %s\n' "$*" >&2
  fi
  exit 1
}

preserve_runtime=false
application_only=false
source_base=
generation=
writer_revision=
recovered_from=
case "$#:${1:-}" in
  0:) ;;
  1:--preserve-runtime) preserve_runtime=true ;;
  1:--application-only) application_only=true; preserve_runtime=true ;;
  2:--source-only)
    [[ "$2" =~ ^[0-9a-f]{40}$ ]] || die "source-only requires the full installed commit"
    source_base="$2"; application_only=true; preserve_runtime=true ;;
  5:--protected-source|7:--supersede-unstarted)
    [[ "$2" =~ ^[0-9a-f]{40}$ && "$3" =~ ^[0-9a-f]{40}$ && "$4" =~ ^[0-9a-f]{40}$ ]] ||
      die "protected-source requires full installed, candidate and tool commits"
    source_base="$2"; generation="$3"; tool_revision="$4"; REPO_ROOT="$5"
    if [[ "$1" == --supersede-unstarted ]]; then
      [[ "$6" =~ ^[0-9a-f]{40}$ && "$6" != "$source_base" &&
        "$7" =~ ^hololive-api-v2-[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9]+$ ]] || die "invalid unstarted writer revision or API release"
      writer_revision="$6"; failed_release="$7"
    fi
    application_only=true; preserve_runtime=true ;;
  *) die "usage: $0 [--preserve-runtime | --application-only | --source-only <installed-commit> | --protected-source <installed-commit> <candidate-commit> <tool-commit> <candidate-clone> | --supersede-unstarted <source-commit> <candidate-commit> <tool-commit> <candidate-clone> <writer-commit> <failed-release-basename>]" ;;
esac

[[ "$(id -u)" -eq 0 ]] || die "must run as root (sudo)"


assert_clean() {
  local dir="$1" name="$2"
  git -C "$dir" rev-parse --git-dir >/dev/null 2>&1 || die "$name git tree missing: $dir"
  if [[ -n "$(git -C "$dir" status --porcelain --untracked-files=no)" ]]; then
    git -C "$dir" status --short --untracked-files=no >&2
    die "$name has uncommitted tracked changes; commit/stash before sync"
  fi
}

source_authority() {
  python3 -I -S - "$@" <<'PY'
import hashlib
import os
import stat
import sys
from pathlib import Path

kind, *names = sys.argv[1:]
seen = set()
data_root = Path("/opt/hololive-bot/compose/current")

def check(path, tree=False):
    if path in seen:
        return
    info = path.lstat()
    directory = stat.S_ISDIR(info.st_mode)
    data = kind == "protected" and data_root in path.parents
    if (info.st_uid or info.st_gid or info.st_mode & (0o7002 if data else 0o7022) or
            not directory and not (stat.S_ISREG(info.st_mode) and info.st_nlink == 1) or
            any(k.startswith("system.posix_acl_") or k == "security.capability" for k in os.listxattr(path))):
        sys.exit(f"unsafe protected-source authority: {path}")
    if not tree and not directory:
        sys.exit(f"not a protected-source directory: {path}")
    seen.add(path)
    if tree and kind == "protected":
        digest = ""
        if not directory:
            with path.open("rb") as src:
                digest = hashlib.file_digest(src, "sha256").hexdigest()
        print(path, info.st_dev, info.st_ino, info.st_mode, info.st_mtime_ns, info.st_ctime_ns, digest)
    if tree and directory:
        for child in sorted(path.iterdir()):
            check(child, True)

for name in names:
    path = Path(name)
    if not path.is_absolute() or path.resolve(strict=False) != path:
        sys.exit(f"invalid protected-source path: {path}")
    for parent in reversed(path.parents):
        check(parent)
    if kind == "files" and not stat.S_ISREG(path.lstat().st_mode):
        sys.exit(f"not a protected-source regular file: {path}")
    check(path, kind != "directory")
PY
}
if [[ -n "$generation" ]]; then
  [[ "$(hostname -s)" == hololive-seoul && -d /run/lock ]] || die "protected-source requires the existing central host"
  [[ -f /run/lock/hololive-quadlet-redeploy.lock && ! -L /run/lock/hololive-quadlet-redeploy.lock ]] ||
    die "protected-source requires the existing deployment lock"
  lock=/run/lock/hololive-quadlet-redeploy.lock
  source_authority clone "$lock"
  exec 8</run/lock
  exec 9<"/proc/self/fd/8/${lock##*/}"
else
  install -d -m 0755 /run/lock
  exec 9>/run/lock/hololive-quadlet-redeploy.lock
fi
acquire_deployment_lock() {
  flock -n 9 || die "another central Quadlet writer is running"
  if [[ -n "$generation" ]]; then
    [[ /run/lock -ef /proc/self/fd/8 && "$lock" -ef /proc/self/fd/9 ]] || die "deployment lock identity changed"
  fi
}
if [[ -z "$writer_revision" ]]; then
  if [[ -n "$generation" ]]; then
    exec 19</opt
    flock -n 19 && [[ /opt -ef /proc/self/fd/19 ]] || die "recovery supervisor lock unavailable"
  fi
  acquire_deployment_lock
fi

require_publication_window() {
  /usr/bin/python3 -ISB "$tool_root/scripts/deploy/lib/unstarted-source.py" --window publish \
    "$source_base" "$generation" "$writer_revision" "$failed_release" || die "no live supervisor for this source publication"
}

if [[ "$(hostname -s)" == kapu && "${HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED:-}" != 1 ]]; then
  die "refusing to install central Quadlet units on build-control host kapu; set HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1 only for an approved rollback"
fi

if [[ -n "$generation" ]]; then
  [[ "$OPT_ROOT" == /opt/hololive-bot/compose ]] || die "protected-source requires the installed central tree"
  if compgen -A variable GIT_ >/dev/null; then die "protected-source requires an isolated Git environment"; fi
  export GIT_NO_REPLACE_OBJECTS=1
  export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_ATTR_NOSYSTEM=1 GIT_OPTIONAL_LOCKS=0
  export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null
  umask 077
  source_authority directory "$OPT_CURRENT"
  [[ "$(stat -c %a "$OPT_CURRENT")" == 700 ]] || die "protected-source requires the private legacy root"
  store="$OPT_ROOT/app-sources"
  predecessor="$OPT_CURRENT"
  if [[ -e "$store" || -L "$store" ]]; then
    source_authority directory "$store"
    [[ "$(stat -c %a "$store")" == 700 && -f "$store/active" ]] || die "incomplete protected-source store"
    source_authority clone "$store/active"
    exec {predecessor_fd}<"$store/active"
    [[ "$store/active" -ef /proc/self/fd/$predecessor_fd && "$(stat -c %a "$store/active")" == 600 ]] || die "protected-source active mode mismatch"
    cmp -s "/proc/self/fd/$predecessor_fd" <(printf '%s\n' "$source_base") || die "protected-source installed revision mismatch"
    predecessor="$store/$source_base"
    if [[ -n "$writer_revision" ]]; then
      source_authority directory "$predecessor"
    else
      source_authority clone "$predecessor"
    fi
  fi
  if [[ -n "$writer_revision" && "$predecessor" == "$OPT_CURRENT" ]]; then die "no first sealed source to supersede"; fi
  [[ ! -e "$store/$generation" && ! -L "$store/$generation" ]] || die "candidate generation already exists"
  [[ ! -e "$store/transition-$source_base-$generation" ]] || die "source transition already recorded"
  for hold in "$OPT_CURRENT/artifacts/worker-profile/releases/.central-writer-hold" /var/lib/hololive-postgres-fence/{fenced,fence.intent}; do
    [[ ! -e "$hold" && ! -L "$hold" ]] || die "protected-source refused during a writer hold or fence"
  done
  tool_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
  for clone in "$REPO_ROOT" "$tool_root"; do
    [[ "$clone" =~ ^/opt/hololive-bot/release-sources/[^/]+/source$ && -d "$clone/.git" &&
      "$(stat -c %a "${clone%/source}")" == 700 &&
      ! -e "$clone/.git/objects/info/alternates" && ! -e "$clone/.git/commondir" ]] ||
      die "protected-source requires independent staged release clones"
    source_authority clone "$clone"
    for path in .git/info/{grafts,attributes} .git/config.worktree .git/shallow; do
      [[ ! -e "$clone/$path" ]] || die "unsupported release clone override"
    done
    config="$(git config --file "$clone/.git/config" --no-includes --list)" || die "cannot read release clone configuration"
    while IFS= read -r entry; do
      case "$entry" in
        core.repositoryformatversion=0|core.filemode=true|core.bare=false|core.logallrefupdates=true|tar.umask=0022) ;;
        *) die "unsupported release clone configuration" ;;
      esac
    done <<<"$config"
    [[ "$(git -C "$clone" rev-parse --show-toplevel)" == "$clone" ]] || die "unexpected release worktree"
    clean="$(git -C "$clone" status --porcelain --untracked-files=all --ignored)"
    [[ -z "$clean" ]] || die "protected-source requires a clean checkout"
    [[ "$REPO_ROOT" != "$tool_root" ]] || break
  done
  [[ "$(git -C "$tool_root" rev-parse --verify 'HEAD^{commit}')" == "$tool_revision" &&
    "$(git hash-object --stdin <"${BASH_SOURCE[0]}")" == "$(git -C "$tool_root" rev-parse "$tool_revision:scripts/deploy/sync-opt-current.sh")" ]] ||
    die "protected-source bootstrap is not the reviewed tool"
  for lib in protected-binds sealed-app; do
    path="scripts/deploy/lib/$lib.sh"
    trusted_blob="$(git -C "$tool_root" rev-parse --verify "$tool_revision:$path")"
    [[ "$trusted_blob" =~ ^[0-9a-f]{40}$ && "$(git hash-object --no-filters "$tool_root/$path")" == "$trusted_blob" ]] ||
      die "protected-source library does not match the reviewed tool: $lib"
  done
  . "$tool_root/scripts/deploy/lib/protected-binds.sh"
  . "$tool_root/scripts/deploy/lib/sealed-app.sh"
fi

assert_clean "$REPO_ROOT" "repo"
HEAD_REVISION="$(git -C "$REPO_ROOT" rev-parse --verify 'HEAD^{commit}')"
HEAD_SHA="$(git -C "$REPO_ROOT" rev-parse --short "$HEAD_REVISION")"
log "repo=$REPO_ROOT HEAD=$HEAD_SHA -> $OPT_CURRENT"
[[ -z "$generation" || "$generation" == "$HEAD_REVISION" ]] || die "protected-source candidate revision mismatch"
if [[ -n "$generation" ]]; then
  capabilities=(scripts/deploy/{quadlet-redeploy-service.sh,quadlet-rollback-service.sh,quadlet-prestart.sh}
    scripts/deploy/lib/{app-source-authority.py,sealed-app.sh,protected-binds.sh,worker-profile-pair.py})
  if [[ -n "$writer_revision" ]]; then
    capabilities+=(scripts/deploy/{sync-opt-current.sh,supersession-window.py} scripts/deploy/lib/{unstarted-source.py,postgres-endpoint.sh})
  fi
  for path in "${capabilities[@]}"; do
    capability="$(git -C "$tool_root" ls-tree "$tool_revision" -- "$path")"
    read -r mode type blob _ <<<"$capability"
    [[ "$mode" =~ ^100(644|755)$ && "$type" == blob && "$blob" =~ ^[0-9a-f]{40}$ &&
      "$(git hash-object --no-filters "$tool_root/$path")" == "$blob" &&
      "$(git -C "$REPO_ROOT" ls-tree "$generation" -- "$path")" == "$capability" ]] ||
      die "candidate cannot consume the reviewed sealed source: $path"
  done
  if [[ -n "$writer_revision" ]]; then
    require_publication_window
    acquire_deployment_lock
  fi
fi
if [[ -n "$source_base" ]]; then
  [[ -z "$(git -C "$REPO_ROOT" status --porcelain --untracked-files=all)" ]] || die "source-only requires a clean checkout"
  stamp="$OPT_CURRENT/.source-revision"
  [[ -z "$generation" ]] || stamp="$predecessor/.source-revision"
  [[ -f "$stamp" && ! -L "$stamp" ]] &&
    cmp -s "$stamp" <(printf '%s\n' "$source_base") || die "source-only installed revision mismatch"
  [[ "$source_base" != "$HEAD_REVISION" ]] && git -C "$REPO_ROOT" merge-base --is-ancestor "$source_base" "$HEAD_REVISION" ||
    die "source-only requires a descendant of the installed revision"
  reserved="$(git -C "$REPO_ROOT" ls-tree "$source_base" -- .source-revision)$(git -C "$REPO_ROOT" ls-tree "$HEAD_REVISION" -- .source-revision)"
  [[ -z "$reserved" ]] || die "reserved source-revision path in Git tree"
  [[ -n "$generation" || "$(git -C "$REPO_ROOT" hash-object --stdin <"${BASH_SOURCE[0]}")" == "$(git -C "$REPO_ROOT" rev-parse "$HEAD_REVISION:scripts/deploy/sync-opt-current.sh")" ]] ||
    die "source-only bootstrap must be the reviewed coordinator"
fi

if [[ "$preserve_runtime" == true ]]; then
  for d in data logs backups runtime-config; do
    [[ -d "$OPT_CURRENT/$d" ]] || die "$1 requires an existing runtime directory: $OPT_CURRENT/$d"
  done
fi

backup_current() {
  local destination
  local -a exclusions=(--exclude=/data --exclude=/logs --exclude=/backups --exclude=/artifacts/worker-profile)
  [[ -d "$OPT_CURRENT" ]] || return 0
  destination="$(mktemp -d "${OPT_CURRENT}.bak-${HEAD_SHA}-XXXXXX")"
  [[ "$preserve_runtime" != true ]] || exclusions+=(--exclude=/runtime-config)
  rsync -a "${exclusions[@]}" "$OPT_CURRENT/" "$destination/"
  backup="$destination"
  log "backup -> $backup (previous recovery points retained)"
}

if [[ "$application_only" != true ]]; then
  backup_current
fi

mkdir -p "$OPT_ROOT"

STAGING="$(mktemp -d "${OPT_ROOT}/.staging-current.XXXXXX")"
QUADLET_CHECK="$(mktemp -d "${OPT_ROOT}/.quadlet-check.XXXXXX")"
publication=preparing
publication_diagnostic() {
  python3 -ISB -c 'import fcntl, os, sys
try:
    fd = int(sys.argv[1])
    flags = fcntl.fcntl(fd, fcntl.F_GETFL)
    if flags & os.O_ACCMODE != os.O_RDONLY:
        fcntl.fcntl(fd, fcntl.F_SETFL, flags | os.O_NONBLOCK)
        try:
            os.write(fd, (sys.argv[2] + "\n").encode())
        finally:
            fcntl.fcntl(fd, fcntl.F_SETFL, flags)
except OSError:
    pass' "${diagnostic_fd:-2}" "$1" 2>/dev/null || :
}
trap '' XFSZ
cleanup() {
  local status=$?
  trap '' PIPE XFSZ
  if [[ -n "$generation" && "$publication" != preparing && "$publication" != complete ]]; then
    trap '' "${publication_signals[@]}"
    publication_diagnostic "[OUTCOME] outcome_unknown phase=$publication old=$source_base candidate=$generation recovery=$QUADLET_CHECK"
    exit 75
  fi
  if ! rm -rf "$STAGING" "$QUADLET_CHECK"; then
    if [[ "$publication" == complete ]]; then
      publication_diagnostic "[WARN] cleanup_failed after publication candidate=$generation path=$QUADLET_CHECK"
    else
      return 1
    fi
  fi
  return "$status"
}
publication_signals=(HUP INT QUIT TRAP ABRT BUS USR1 USR2 PIPE ALRM TERM STKFLT XCPU XFSZ VTALRM PROF IO PWR SYS)
for signal_number in $(seq "$(kill -l RTMIN)" "$(kill -l RTMAX)"); do publication_signals+=("$(kill -l "$signal_number")"); done
trap cleanup EXIT
publication_signal_exit() {
  [[ $publication == preparing ]] && exit $((128 + $(kill -l "$1")))
  exit 75
}
if [[ -n "$generation" ]]; then
  for signal in "${publication_signals[@]}"; do
    # shellcheck disable=SC2064
    trap "publication_signal_exit $signal" "$signal"
  done
fi
if [[ -n "$source_base" ]]; then
  changes="$QUADLET_CHECK/changes"
  git -C "$REPO_ROOT" diff --no-renames --no-ext-diff --raw --no-abbrev -z "$source_base" "$HEAD_REVISION" >"$changes"
  replacements=()
  if [[ -n "$writer_revision" ]]; then
    sealed_supersession_delta "$REPO_ROOT" "$HEAD_REVISION" || die "unapproved supersession delta from approved parent"
  fi
  while IFS= read -r -d '' header && IFS= read -r -d '' path; do
    read -r old_mode new_mode old_blob new_blob status <<<"${header#:}"
    if [[ -n "$generation" ]]; then
      case "$status:$old_mode:$new_mode" in
        M:100644:100644|M:100755:100755|A:000000:100644|A:000000:100755|D:100644:000000|D:100755:000000) ;;
        *) die "protected-source requires reviewed regular files without mode changes" ;;
      esac
      if [[ -n "$writer_revision" ]]; then
        refusal=$(sealed_supersession_path "$status" "$new_mode" "$path") || die "$refusal"
      fi
      case "$path" in
        hololive/hololive-api/scripts/init-db/*|scripts/ci/postgres-capacity-policy.tsv|scripts/deploy/verify-exec-tree-ownership.sh|scripts/deploy/lib/source-revision.sh) die "protected source path: $path" ;;
        verification/tla/AlarmDispatchClaim/COVERAGE.md|verification/tla/CollectionJobLease/COVERAGE.md)
          [[ "$status:$old_mode:$new_mode" == M:100644:100644 ]] || die "protected-source coverage requires an existing non-executable document" ;;
        README.md|CHANGELOG.md|VERSION|go.work|go.work.sum|.github/workflows/*.yml|docs/*|hololive/*|scripts/README.md|scripts/ci/*|scripts/deploy/*|scripts/logs/*|deploy/quadlet/README.md|deploy/compose/README.md|deploy/podman/iris-seoul/README.md|deploy/podman/kapu/README.md|deploy/quadlet/hololive-bot/hololive-db-migrate.container) ;;
        *) die "unapproved protected-source path: $path" ;;
      esac
      continue
    fi
    [[ "$status" == M && "$old_mode" == "$new_mode" && "$old_mode" =~ ^100(644|755)$ ]] || die "source-only requires regular-file modifications"
    case "$path" in
      hololive/*/scripts/*|hololive/*/internal/migrationrunner/*|scripts/ci/postgres-capacity-policy.tsv) die "protected source path: $path" ;;
      README.md|CHANGELOG.md|VERSION|go.work|go.work.sum|.github/workflows/*.yml|docs/*|deploy/compose/README.md|hololive/*|scripts/README.md|scripts/ci/*|scripts/deploy/sync-opt-current.sh|scripts/deploy/sync-opt-current_test.sh) ;;
      *) die "unapproved source-only path: $path" ;;
    esac
    live="$OPT_CURRENT/$path"
    [[ -f "$live" && ! -L "$live" && "$(realpath -e "$live")" == "$live" && "$(stat -c %h "$live")" == 1 &&
      "$(git hash-object --no-filters "$live")" == "$old_blob" && "$(git hash-object --no-filters "$REPO_ROOT/$path")" == "$new_blob" ]] ||
      die "source-only content or path mismatch: $path"
    replacements+=("$path")
  done <"$changes"
  [[ -s "$changes" ]] || die "source-only has no replacements"
  if [[ -n "$generation" ]]; then
    revisions=("$source_base" "$HEAD_REVISION")
    if [[ -n "$writer_revision" ]]; then revisions+=("$writer_revision"); fi
    for revision in "${revisions[@]}"; do
      git -C "$REPO_ROOT" ls-tree -rlz "$revision" >"$QUADLET_CHECK/$revision.tree"
      while IFS= read -r -d '' entry; do
        [[ "$entry" == '100644 blob '* || "$entry" == '100755 blob '* ]] || die "non-regular file in protected-source revision"
      done <"$QUADLET_CHECK/$revision.tree"
    done
  else
  python3 -I -S - "$OPT_CURRENT" <<'PY'
import os
import stat
import sys
from pathlib import Path

root = Path(sys.argv[1])
programs = [
    "scripts/deploy/quadlet-redeploy-service.sh", "scripts/deploy/quadlet-rollback-service.sh",
    "scripts/deploy/quadlet-prestart.sh", "scripts/ci/postgres-capacity-policy.tsv",
    *("scripts/deploy/lib/" + name for name in (
        "worker-profile-pair.py", "systemd-stop-guard.py", "card-migration-guard.py", "admin-bind.sh",
        "public-bind-mounts.sh", "postgres-capacity.sh", "compose-env.sh", "postgres-endpoint.sh")),
]
files = {root / path for path in programs}
installed = Path("/usr/local/libexec/hololive-bot")
files.add(installed / "quadlet-prestart")
files.update(installed / "lib" / name for name in (
    "admin-bind.sh", "public-bind-mounts.sh", "postgres-capacity.sh", "compose-env.sh", "postgres-endpoint.sh"))
directories = {parent for path in files for parent in path.parents}
for path in sorted(directories | files):
    info = path.lstat()
    kind_ok = stat.S_ISDIR(info.st_mode) if path in directories else stat.S_ISREG(info.st_mode) and info.st_nlink == 1
    if (not kind_ok or info.st_uid != 0 or info.st_mode & 0o022 or
            any(name.startswith("system.posix_acl_") for name in os.listxattr(path, follow_symlinks=False))):
        sys.exit(f"unsafe installed coordinator authority: {path}")
PY
  fi
fi
candidate_images() {
  local service record image actual
  for service in hololive-api hololive-alarm-worker; do
    record="${REPO_ROOT%/source}/$service.candidate-image"
    source_authority clone "$record" || return 1
    image="$(<"$record")"
    [[ "$image" =~ ^sha256:[0-9a-f]{64}$ ]] && cmp -s "$record" <(printf '%s\n' "$image") || die "invalid staged $service image identity"
    actual="$(podman image inspect --format '{{.Id}} {{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.title"}} {{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" || return 1
    [[ "$actual" == "${image#sha256:} linux/arm64 $service $generation" ]] || die "staged $service image does not match the candidate generation"
    printf '%s %s\n' "$service" "$image"
  done
}
verify_snapshot() {
  python3 -I -S - "$QUADLET_CHECK/$1.tree" "$1" "$2" "$3" "${4:-}" "${5:-}" "${@:6}" <<'PY'
import hashlib
import json
import os
import stat
import sys
from pathlib import Path

tree, revision, source, kind, backup, record, *originals = sys.argv[1:]
originals = [Path(p) for p in originals]
copied = kind in ("backup", "sealed-backup")
expected = {}
for entry in Path(tree).read_bytes().split(b"\0")[:-1]:
    header, name = entry.split(b"\t", 1)
    mode, _, digest, size = header.decode().split()
    expected[Path(os.fsdecode(name))] = (mode == "100755", digest, int(size))
stamp = (revision + "\n").encode()
expected[Path(".source-revision")] = (False, hashlib.sha1(b"blob 41\0" + stamp).hexdigest(), len(stamp))
excluded = {Path(p) for p in ("data", "logs", "backups", "runtime-config", "artifacts/worker-profile")} if kind in ("legacy", "backup") else set()
expected = {p: value for p, value in expected.items() if not any(p == e or e in p.parents for e in excluded)}
dirs = {parent for p in expected for parent in p.parents}
if kind == "legacy":
    dirs.add(Path("artifacts"))
root = Path(source)
seen = set()
def fail(path):
    sys.exit(f"protected-source {kind} archive or metadata mismatch: {path}")

def inspect(fd, rel, directory):
    info = os.fstat(fd)
    attrs = os.listxattr(fd)
    mode = stat.S_IMODE(info.st_mode)
    if (info.st_uid or info.st_gid or mode & 0o7002 or
            any(k.startswith("system.posix_acl_") or k == "security.capability" for k in attrs) or
            (not stat.S_ISDIR(info.st_mode) if directory else not stat.S_ISREG(info.st_mode) or info.st_nlink != 1)):
        fail(rel)
    if kind == "sealed":
        required = (0o500 if rel == Path(".") else 0o555) if directory else (
            0o400 if rel == Path(".source-revision") else 0o555 if expected[rel][0] else 0o444)
        if mode != required:
            fail(rel)
    if copied and (mode != (0o700 if directory else 0o600) or
                   any(os.path.samestat(info, (p / rel).lstat()) for p in originals)):
        fail(rel)
    if backup or record:
        labels = {k: hashlib.sha256(os.getxattr(fd, k)).hexdigest() for k in sorted(attrs)}
        print(json.dumps([str(rel), info.st_dev, info.st_ino, mode, info.st_uid, info.st_gid,
                          info.st_nlink, info.st_mtime_ns, info.st_ctime_ns, labels]))
    return info

def onerror(err):
    raise err

for base, subdirs, files, dfd in os.fwalk(root, follow_symlinks=False, onerror=onerror):
    rel = Path(base).relative_to(root)
    if rel not in dirs:
        fail(rel)
    inspect(dfd, rel, True)
    subdirs[:] = sorted(d for d in subdirs if kind != "legacy" or rel / d not in excluded)
    for child in subdirs:
        if rel / child not in dirs or not stat.S_ISDIR(os.stat(child, dir_fd=dfd, follow_symlinks=False).st_mode):
            fail(rel / child)
    for name in sorted(files):
        p = rel / name
        if p not in expected or not stat.S_ISREG(os.stat(name, dir_fd=dfd, follow_symlinks=False).st_mode):
            fail(p)
        with os.fdopen(os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=dfd), "rb") as f:
            info = inspect(f.fileno(), p, False)
            executable, blob, size = expected[p]
            if info.st_size != size or (not copied and bool(info.st_mode & 0o111) != executable):
                fail(p)
            data = f.read(size + 1)
            digest = hashlib.sha1(b"blob %d\0" % size)
            digest.update(data)
            if (len(data) != size or digest.hexdigest() != blob or
                    os.fstat(f.fileno()).st_ctime_ns != info.st_ctime_ns):
                fail(p)
        seen.add(p)
        if backup:
            dest = Path(backup) / p
            dest.parent.mkdir(parents=True, exist_ok=True)
            with dest.open("xb") as dst:
                dst.write(data)
if seen != set(expected):
    fail(min(set(expected) - seen))
PY
}

seal_source() {
  printf '%s\n' "$2" >"$1/.source-revision"
  chmod 0400 "$1/.source-revision"
  chmod 0500 "$1"
  verify_snapshot "$2" "$1" sealed
}

classify_predecessor() {
  local -a receipts=("$store"/transition-*-"$source_base") fields
  local dir rev old tool
  [[ ${#receipts[@]} == 1 ]] || die "ambiguous predecessor transition"
  dir="${receipts[0]}"; rev="$dir/revisions"; old="${dir##*/transition-}"; old="${old%-"$source_base"}"
  [[ "$old" =~ ^[0-9a-f]{40}$ && "$old" != "$source_base" ]] || die "missing predecessor transition"
  source_authority files "$rev"
  mapfile -t fields <"$rev"
  [[ ${fields[2]:-} =~ ^tool=([0-9a-f]{40})$ ]] || die "invalid predecessor tool receipt"
  tool="${BASH_REMATCH[1]}"
  if cmp -s "$rev" <(printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$old" "$source_base" "$tool"); then
    sealed_ordinary_transition "$dir" "/opt/hololive-bot/release-sources/dv-tool-$source_base/source.window" ||
      die "ordinary predecessor has recovery evidence"
    return 0
  fi
  [[ ${fields[3]:-} =~ ^running=([0-9a-f]{40})$ && "$tool" == "$source_base" ]] &&
    cmp -s "$rev" <(printf 'installed=%s\ncandidate=%s\ntool=%s\nrunning=%s\n' "$old" "$source_base" "$tool" "${BASH_REMATCH[1]}") ||
    die "invalid recovered predecessor transition"
  source_authority files "$dir/predecessor.writers"
  recovered_from="$old"
}

require_completed_recovery() {
  /usr/bin/python3 -ISB "$predecessor/scripts/deploy/lib/unstarted-source.py" --completed \
    "$recovered_from" "$source_base" "$@" || die "original recovery windows have no verified terminal acceptance"
}

exchange_pointer() {
  local replacement=$1 original_fd=$2 expected=$3 identity=$4
  cmp -s "$identity" <(stat -Lc '%d %i' "/proc/self/fd/$original_fd" && printf '%s\n' "$expected") || return 75
  [[ ! -L "$store/active" && "$store/active" -ef "/proc/self/fd/$original_fd" ]] &&
    cmp -s "$store/active" <(printf '%s\n' "$expected") || return 75
  mv -T --exchange --no-copy "$replacement" "$store/active" || return 75
  if [[ -L "$replacement" || ! "$replacement" -ef "/proc/self/fd/$original_fd" ]] ||
      ! cmp -s "$replacement" <(printf '%s\n' "$expected"); then
    sync -f "$OPT_ROOT" || :
    return 75
  fi
}

publish_generation() {
  local marker_fd kind=sealed receipt="$store/transition-$source_base-$generation" stage="$QUADLET_CHECK/transition"
  [[ "$predecessor" != "$OPT_CURRENT" ]] || kind=legacy
  mkdir -m0700 -- "$stage" "$stage/previous.actual"
  candidate_images >"$stage/images"
  verify_snapshot "$source_base" "$predecessor" "$kind" "$stage/previous.actual" >"$stage/previous.metadata"
  if [[ "$kind" == legacy ]]; then
    seal_source "$previous" "$source_base"
  else
    cp -- "$QUADLET_CHECK/pairs.before" "$stage/pairs.before"
  fi
  cp -- "$changes" "$stage/changes.raw"
  cp -- "$QUADLET_CHECK/protected.before" "$stage/protected.before"
  printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$source_base" "$generation" "$tool_revision" >"$stage/revisions"
  if [[ -n "$writer_revision" ]]; then
    printf 'running=%s\n' "$writer_revision" >>"$stage/revisions"
    cp -- "$QUADLET_CHECK/predecessor.writers" "$stage/predecessor.writers"
  fi
  printf 'incomplete %s -> %s\n' "$source_base" "$generation" >"$QUADLET_CHECK/incomplete"
  exec {marker_fd}<"$QUADLET_CHECK/incomplete"
  { stat -Lc '%d %i' "/proc/self/fd/$marker_fd" && cat "$QUADLET_CHECK/incomplete"; } >"$stage/incomplete.identity"
  if [[ $kind == sealed ]]; then
    { stat -Lc '%d %i' "/proc/self/fd/$predecessor_fd" && printf '%s\n' "$source_base"; } >"$stage/predecessor.identity"
  fi
  mv --help >"$QUADLET_CHECK/mv.options"
  grep -Fq -- --exchange "$QUADLET_CHECK/mv.options" || die 'atomic exchange is unavailable'
  printf '%s\n' "$generation" >"$QUADLET_CHECK/active"
  if [[ "$kind" == legacy ]]; then
    mkdir -m0700 "$QUADLET_CHECK/new-store"
    rename_absent "$QUADLET_CHECK/incomplete" "$QUADLET_CHECK/new-store/active"
    rename_absent "$stage" "$QUADLET_CHECK/new-store/${receipt##*/}"
  fi
  sync -f "$OPT_ROOT"
  if [[ -e /proc/self/fd/2 ]]; then exec {diagnostic_fd}>&2; fi
  exec 2>/dev/null
  if [[ "$kind" == legacy ]]; then
    publication=invalidating
    rename_absent "$QUADLET_CHECK/new-store" "$store"
  else
    if [[ -n "$writer_revision" ]]; then require_publication_window; fi
    publication=invalidating
    exchange_pointer "$QUADLET_CHECK/incomplete" "$predecessor_fd" "$source_base" "$stage/predecessor.identity"
  fi
  sync -f "$OPT_ROOT"
  publication=incomplete
  if [[ "$kind" != legacy ]]; then rename_absent "$stage" "$receipt"; fi
  if [[ "$kind" == legacy ]]; then rename_absent "$previous" "$store/$source_base"; fi
  rename_absent "$STAGING" "$store/$generation"
  verify_snapshot "$generation" "$store/$generation" sealed
  verify_snapshot "$source_base" "$predecessor" "$kind" "" record >"$receipt/previous.after"
  cmp -s "$receipt/previous.metadata" "$receipt/previous.after" || die "predecessor metadata changed"
  protected_inventory "$store/$generation" >"$receipt/protected.after"
  cmp -s "$receipt/protected.before" "$receipt/protected.after" || die "protected inventory changed"
  candidate_images >"$receipt/images.after"
  cmp -s "$receipt/images" "$receipt/images.after" || die "candidate image custody changed"
  if [[ -n "$writer_revision" ]]; then
    verify_snapshot "$source_base" "$receipt/previous.actual" sealed-backup "" "" "$predecessor"
    check_unstarted_source committing "$receipt/protected.after" "$QUADLET_CHECK/writers.after" "$receipt/pairs.after"
    cmp -s "$receipt/pairs.before" "$receipt/pairs.after" && cmp -s "$receipt/predecessor.writers" "$QUADLET_CHECK/writers.after" ||
      die "original writer evidence changed during publication"
  elif [[ "$kind" == sealed ]]; then
    [[ "$(accepted_migrator "$receipt/pairs.after")" == "$accepted_image" ]] &&
      cmp -s "$receipt/pairs.before" "$receipt/pairs.after" || die "accepted writer pair changed during publication"
  fi
  sync -f "$OPT_ROOT"
  if [[ -n "$recovered_from" ]]; then
    require_completed_recovery "$generation"
  fi
  if [[ "$kind" == sealed && -z "$writer_revision" ]]; then
    [[ "$(accepted_migrator "$QUADLET_CHECK/final.pairs")" == "$accepted_image" ]] &&
      cmp -s "$receipt/pairs.after" "$QUADLET_CHECK/final.pairs" || die "accepted writers changed at active exchange"
  fi
  if [[ -n "$writer_revision" ]]; then
    require_publication_window
    . "$tool_root/scripts/deploy/lib/postgres-endpoint.sh"
    mapfile -t final_writers <"$receipt/predecessor.writers"
    [[ ${#final_writers[@]} == 2 ]] || die "writer changed before active publication"
    sealed_check_writer_states "${final_writers[@]}"
  fi
  local candidate_fd
  exec {candidate_fd}<"$QUADLET_CHECK/active"
  publication=committing
  exchange_pointer "$QUADLET_CHECK/active" "$marker_fd" "incomplete $source_base -> $generation" "$receipt/incomplete.identity"
  publication=confirming
  sync -f "$OPT_ROOT"
  [[ ! -L "$store/active" && "$store/active" -ef "/proc/self/fd/$candidate_fd" &&
    "$(stat -c '%F %h %u %g %a' -- "$store/active")" == "regular file 1 0 0 600" ]] &&
    cmp -s "$store/active" <(printf '%s\n' "$generation") || die "final source generation mismatch"
  exec {candidate_fd}<&-
  log "published $store/$generation; previous source and evidence: $receipt; runtime unchanged"
  publication=complete
}

archive_config=()
[[ -z "$generation" ]] || archive_config=(-c tar.umask=0222)
git -C "$REPO_ROOT" "${archive_config[@]}" archive "$HEAD_REVISION" | tar -x -C "$STAGING"
if [[ -n "$generation" ]]; then
  seal_source "$STAGING" "$HEAD_REVISION"
else
  printf '%s\n' "$HEAD_REVISION" >"$STAGING/.source-revision"
fi
log "staged tracked tree ($(find "$STAGING" -type f | wc -l) files)"

if [[ "$preserve_runtime" == true ]]; then
  bash "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "$OPT_CURRENT" ||
    die "existing execution tree ownership is unsafe; nothing was installed"
  if [[ -z "$generation" ]]; then
    chmod --reference="$OPT_CURRENT" "$STAGING"
    chown --reference="$OPT_CURRENT" "$STAGING"
  fi
fi

migrator=hololive-db-migrate.container
migrator_path="deploy/quadlet/hololive-bot/$migrator"
host_assets=(
  "scripts/deploy/quadlet-prestart.sh|$LIBEXEC_DIR/quadlet-prestart|755"
  "scripts/systemd/admin-dashboard-ingress-firewall.service|/etc/systemd/system/admin-dashboard-ingress-firewall.service|644"
  "scripts/systemd/admin-dashboard-ingress.nft|/etc/nftables.d/admin-dashboard-ingress.nft|644"
)
for lib in admin-bind public-bind-mounts postgres-capacity compose-env postgres-endpoint; do
  host_assets+=("scripts/deploy/lib/$lib.sh|$LIBEXEC_DIR/lib/$lib.sh|644")
done

assert_same_file() {
  [[ -f "$1" && ! -L "$1" && -f "$2" && ! -L "$2" ]] && cmp -s -- "$1" "$2" ||
    die "application-only file drift: $2"
}

assert_same_tree() {
  local source="$1" dest="$2" changes
  shift 2
  [[ -d "$source" && ! -L "$source" && -d "$dest" && ! -L "$dest" ]] ||
    die "application-only tree drift: $dest"
  changes="$(rsync -rlDEcni --delete "$@" "$source/" "$dest/")"
  [[ -z "$changes" ]] || die "application-only tree drift: $dest ($changes)"
}

assert_installed_migrator() {
  local source="$1" unit="$QUADLET_DIR/$migrator"
  if [[ -z "$generation" ]] || cmp -s "$source" "$unit"; then
    assert_same_file "$source" "$unit"
    return
  fi
  python3 -I -S - "$unit" "$source" <<'PY'
import sys
from pathlib import Path

installed, candidate = (Path(p).read_bytes().splitlines(keepends=True) for p in sys.argv[1:])
retired = {
    b"Volume=/opt/hololive-bot/compose/current/hololive/hololive-api/scripts/migrations:/migrations:ro\n",
    b"Environment=MIGRATION_BASELINE_THROUGH=${MIGRATION_BASELINE_THROUGH}\n",
    b"Environment=MIGRATION_ALLOW_BLOCKING_INDEX_DROP=${MIGRATION_ALLOW_BLOCKING_INDEX_DROP}\n",
    b"Environment=MIGRATION_BASELINE_THROUGH=\n",
    b"Environment=MIGRATION_ALLOW_BLOCKING_INDEX_DROP=false\n",
}
if any(installed.count(line) != 1 for line in retired) or candidate != [line for line in installed if line not in retired]:
    sys.exit("incompatible installed schema-only migrator")
PY
}

rename_absent() {
  python3 -I -S - "$1" "$2" <<'PY'
import ctypes
import os
import sys

rename = ctypes.CDLL(None, use_errno=True).renameat2
if rename(-100, os.fsencode(sys.argv[1]), -100, os.fsencode(sys.argv[2]), 1) != 0:
    raise OSError(ctypes.get_errno(), "rename without replacement", sys.argv[2])
PY
}

accepted_migrator() {
  local dropin="$QUADLET_DIR/$migrator.d/50-migrator-image.conf" image actual
  [[ -f "$dropin" && "$(stat -c '%u:%g:%a:%h' "$dropin")" == 0:0:644:1 ]] || die "missing accepted migrator drop-in"
  image="$(awk -F= '/^\[/{s=$0} s=="[Container]" && $1=="Image"{print $2}' "$dropin")"
  [[ "$image" =~ ^sha256:[0-9a-f]{64}$ ]] || die "invalid accepted migration image"
  sealed_migrator_dropin "$predecessor" "$image" >"$QUADLET_CHECK/accepted-migrator.conf" || die "cannot render accepted migration definition"
  cmp -s "$dropin" "$QUADLET_CHECK/accepted-migrator.conf" || die "installed migration definition differs from the active generation"
  actual="$(podman image inspect --format '{{.Id}} {{index .Config.Labels "org.opencontainers.image.revision"}} {{index .Config.Labels "org.opencontainers.image.title"}}' "$image")" || return 1
  [[ "$actual" == "${image#sha256:} $source_base hololive-api" ]] || die "accepted migration image identity differs"
  record_writer_pairs sealed "$source_base" "$image" "$1" || return 1
  printf '%s\n' "$image"
}

record_writer_pairs() {
  local kind="$1" revision="$2" image="$3" records="$4" service pointer pair release app installed check_cid check_image actual path
  local tool="$tool_root/scripts/deploy/lib/worker-profile-pair.py"
  : >"$records" || return 1
  for service in hololive-api hololive-alarm-worker; do
    pointer="$OPT_CURRENT/artifacts/worker-profile/active/$service.pair"
    [[ -f "$pointer" && "$(realpath -e "$pointer")" == "$pointer" ]] || die "missing active $service pair"
    source_authority clone "$pointer" || return 1
    pair="$(<"$pointer")"
    cmp -s "$pointer" <(printf '%s\n' "$pair") && [[ "$(stat -c '%u:%g:%h' "$pointer")" == 0:0:1 ]] ||
      die "invalid active $service pointer"
    release="${pair%/candidate}"
    [[ "$pair" == "$release/candidate" && "${release%/*}" == "$OPT_CURRENT/artifacts/worker-profile/releases" &&
      "${release##*/}" == "$service-v2-"* && "$(realpath -e "$pair")" == "$pair" ]] || die "invalid active $service release"
    installed="$QUADLET_DIR/$service.container.d/50-worker-profile.conf"
    if [[ "$kind" == legacy ]]; then
      [[ "${release##*/}" =~ ^"$service"-v2-[0-9]{8}T[0-9]{6}Z-[A-Za-z0-9]+$ ]] || die "invalid original $service release"
      python3 -ISB "$tool_root/scripts/deploy/lib/unstarted-source.py" --legacy-release "$pair" || return 1
    else
      source_authority clone "$pair" || return 1
    fi
    source_authority files "$installed" "$release"/{candidate.image,candidate.revision,migrator.image,service} || return 1
    if [[ "$kind" == sealed ]]; then
      source_authority clone "$release/schema-check" || return 1
      read -r check_cid check_image <"$release/schema-check" &&
        [[ "$check_cid" =~ ^[0-9a-f]{64}$ && "$check_image" == "$image" ]] &&
        cmp -s "$release/schema-check" <(printf '%s %s\n' "$check_cid" "$image") || die "invalid accepted $service schema-check receipt"
    else
      for path in schema-job writer-job schema-check accepted-writer activation.intent; do
        [[ ! -e "$release/$path" && ! -L "$release/$path" ]] || die "active $service is not an unstarted legacy predecessor"
      done
    fi
    cmp -s "$release/candidate.revision" <(printf '%s\n' "$revision") &&
      cmp -s "$release/migrator.image" <(printf '%s\n' "$image") &&
      cmp -s "$release/service" <(printf '%s\n' "$service") || die "active $service receipt mismatch"
    app="$(<"$release/candidate.image")"
    cmp -s "$release/candidate.image" <(printf '%s\n' "$app") || die "invalid active $service image record"
    actual="$(podman container inspect --format '{{.Image}} {{index .Config.Labels "org.opencontainers.image.revision"}} {{.State.Health.Status}}' "$service")" || return 1
    [[ "$app" =~ ^sha256:[0-9a-f]{64}$ && "$actual" == "${app#sha256:} $revision healthy" ]] || die "active $service image mismatch"
    if [[ "$kind" == legacy ]]; then
      if [[ "$service" == hololive-api ]]; then
        [[ "$image" == "$app" ]] || die "legacy migrator is not the original API image"
      else
        [[ "$5" == "$app" ]] || die "legacy pair differs from the original worker image"
      fi
      actual="$(podman image inspect --format '{{.Id}} {{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.title"}} {{index .Config.Labels "org.opencontainers.image.revision"}}' "$app")" || return 1
      [[ "$actual" == "${app#sha256:} linux/arm64 $service $revision" ]] || die "legacy $service image identity differs"
    fi
    sealed_installed_pair "$service" "$pair" "$QUADLET_DIR" ||
      die "installed $service pair definition differs"
    python3 -ISB - "$pair" "$app" "$revision" "$kind" <<'PY' || return 1
import json
import re
import sys
from pathlib import Path

pair = Path(sys.argv[1])
metadata = json.loads((pair / "pair.json").read_bytes())
if ((metadata.get("image"), metadata.get("revision")) != (sys.argv[2], sys.argv[3]) or
        (metadata.get("source_generation") != sys.argv[3] if sys.argv[4] == "sealed" else "source_generation" in metadata)):
    sys.exit("active pair is not bound to its expected image, revision and source")
manifest = (pair / "SHA256SUMS").read_text().splitlines()
if len(manifest) != 3 or {line[66:] for line in manifest if re.fullmatch(r"[0-9a-f]{64}  [a-z.]+", line)} != {"profile.json", "pair.json", "unit.container"}:
    sys.exit("invalid sealed pair checksum set")
PY
    python3 -ISB "$tool" verify-dropin "$service" "$pair" "$installed" &&
      python3 -ISB "$tool" verify-live "$service" "$pair" "$service" || die "accepted $service live profile verification failed"
    printf '%s %s\n' "$service" "$pair" >>"$records"
    sha256sum -- "$pair/pair.json" "$installed" >>"$records" || return 1
  done
}

unstarted_migrator() {
  local image="$1" unit=hololive-db-migrate.service fragment
  fragment="$(systemctl show "$unit" --property=FragmentPath --value)"
  [[ "$fragment" == "/run/systemd/generator/$unit" && -f "$fragment" ]] || die "unrecognized generated migrator"
  source_authority files "$fragment"
  /usr/bin/python3 -ISB "$tool_root/scripts/deploy/lib/unstarted-source.py" --migrator \
    "$generated/$unit" "$predecessor" "$image" || die "unstarted migrator differs from the reviewed inactive check"
}

check_unstarted_source() {
  local phase="$1" inventory="$2" writers_file="$3" pairs="$4"
  local prior="$store/transition-$writer_revision-$source_base" old_api old_worker image service name record other
  local -a rows
  source_authority directory "$store" "$prior" "$prior/previous.actual" "$store/$writer_revision"
  [[ -f "$store/active" && "$(stat -c %a "$store/active")" == 600 && "$(stat -c %a "$store")" == 700 ]] || die "unsafe active source pointer"
  source_authority clone "$store/active"
  for name in "$OPT_CURRENT/artifacts/worker-profile/releases/.central-writer-hold" /var/lib/hololive-postgres-fence/{fenced,fence.intent}; do
    [[ ! -e "$name" && ! -L "$name" ]] || die "unstarted supersession refused during writer hold or fence"
  done
  [[ "$(stat -c %a "$prior")" == 700 ]] || die "unsafe first source transition"
  python3 -ISB - "$store" "$writer_revision" "$source_base" "$generation" "$phase" <<'PY'
from pathlib import Path
import sys

store, writer, source, candidate, phase = sys.argv[1:]
store = Path(store)
prior = "transition-" + writer + "-" + source
entries = {"active", writer, source, prior}
active = source
if phase == "committing":
    entries.update([candidate, "transition-" + source + "-" + candidate])
    active = "incomplete " + source + " -> " + candidate
if {p.name for p in store.iterdir()} != entries or (store / "active").read_bytes() != (active + "\n").encode():
    sys.exit("not a direct supersession of the first sealed generation")
if {p.name for p in (store / prior).iterdir()} != {
        "revisions", "previous.actual", "previous.metadata", "previous.after",
        "changes.raw", "protected.before", "protected.after", "images", "images.after"}:
    sys.exit("unexpected first transition records")
PY
  for name in revisions previous.metadata previous.after changes.raw protected.before protected.after images images.after; do
    record="$prior/$name"
    [[ -f "$record" && ! -L "$record" ]] || die "missing first transition record: $name"
    source_authority clone "$record"
  done
  cmp -s "$prior/revisions" <(printf 'installed=%s\ncandidate=%s\ntool=%s\n' "$writer_revision" "$source_base" "$source_base") ||
    die "first publication does not bind the legacy writers to this sealed source"
  git -C "$REPO_ROOT" diff --no-renames --no-ext-diff --raw --no-abbrev -z "$writer_revision" "$source_base" >"$QUADLET_CHECK/first.changes"
  cmp -s "$prior/changes.raw" "$QUADLET_CHECK/first.changes" && cmp -s "$prior/previous.metadata" "$prior/previous.after" &&
    cmp -s "$prior/images" "$prior/images.after" && cmp -s "$prior/protected.before" "$prior/protected.after" ||
    die "first source publication evidence changed"
  verify_snapshot "$writer_revision" "$store/$writer_revision" sealed
  verify_snapshot "$writer_revision" "$prior/previous.actual" backup "" "" "$OPT_CURRENT" "$store/$writer_revision"
  verify_snapshot "$writer_revision" "$OPT_CURRENT" legacy "" record >"$QUADLET_CHECK/legacy.now"
  cmp -s "$prior/previous.metadata" "$QUADLET_CHECK/legacy.now" || die "legacy source or rollback metadata changed"
  python3 -ISB - "$prior/protected.before" "$inventory" "$QUADLET_DIR/$migrator.d" <<'PY'
from pathlib import Path
import sys

before, now, directory = sys.argv[1:]
overlay = directory + "/50-migrator-image.conf"
def stable(path):
    found = set()
    for line in Path(path).read_text().splitlines():
        key = line.split(" ", 1)[0]
        if key not in (overlay, directory):
            yield line
            continue
        if key in found:
            sys.exit("ambiguous first migrator inventory")
        found.add(key)
        fields = line.split()
        if key == directory and len(fields) == 6:
            yield tuple(fields[:4])
        elif key == overlay and len(fields) == 7:
            yield (key, fields[1], fields[3])
        else:
            sys.exit("invalid first migrator inventory")
    if found != {directory, overlay}:
        sys.exit("missing first migrator inventory")
if list(stable(before)) != list(stable(now)):
    sys.exit("original protected bind, host asset or infrastructure generation changed")
PY
  mapfile -t rows <"$prior/images"
  [[ ${#rows[@]} == 2 && ${rows[0]} =~ ^hololive-api\ (sha256:[0-9a-f]{64})$ ]] || die "invalid first API image receipt"
  image="${BASH_REMATCH[1]}"
  [[ ${rows[1]} =~ ^hololive-alarm-worker\ (sha256:[0-9a-f]{64})$ ]] || die "invalid first worker image receipt"
  other="${BASH_REMATCH[1]}"
  [[ "$image" != "$other" ]] && cmp -s "$prior/images" <(printf '%s\n' "${rows[@]}") || die "invalid first source image set"
  for record in "${rows[@]}"; do
    read -r service name <<<"$record"
    record="$(podman image inspect --format '{{.Id}} {{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.title"}} {{index .Config.Labels "org.opencontainers.image.revision"}}' "$name")"
    [[ "$record" == "${name#sha256:} linux/arm64 $service $source_base" ]] || die "first source image drift: $service"
  done
  unstarted_migrator "$image"
  python3 -ISB "$tool_root/scripts/deploy/lib/unstarted-source.py" "$source_base" "$writer_revision" "$failed_release" >"$writers_file"
  mapfile -t rows <"$writers_file"
  [[ ${#rows[@]} == 2 ]] && cmp -s "$writers_file" <(printf '%s\n' "${rows[@]}") || die "invalid original writer evidence"
  local pattern='^([a-z-]+) [0-9a-f]{32} [0-9a-f]{64} ([0-9a-f]{64}) holo-pgbouncer\|6432$'
  for service in hololive-api hololive-alarm-worker; do
    [[ "${rows[0]}" =~ $pattern && "${BASH_REMATCH[1]}" == "$service" ]] || die "invalid original $service generation"
    if [[ "$service" == hololive-api ]]; then old_api="sha256:${BASH_REMATCH[2]}"; else old_worker="sha256:${BASH_REMATCH[2]}"; fi
    rows=("${rows[@]:1}")
  done
  record_writer_pairs legacy "$writer_revision" "$old_api" "$pairs" "$old_worker"
}

protected_inventory() {
  local root="$1" path dest asset state cid pid running health name actual expected
  local -a binds
  local -a files=("$QUADLET_DIR" "$LIBEXEC_DIR" "$OPT_CURRENT/$init_db" "$OPT_CURRENT/deploy/pgbouncer"
    "$OPT_CURRENT/deploy/postgres" "$OPT_CURRENT/deploy/nginx" /run/hololive-bot/pgbouncer/pgbouncer.ini)
  for asset in "${host_assets[@]}"; do
    IFS='|' read -r path dest _ <<<"$asset"
    files+=("$dest" "$OPT_CURRENT/$path")
  done
  source_authority protected "${files[@]}" || return 1
  for path in "$root"/deploy/quadlet/hololive-bot/*; do
    [[ "$(stat -c %a "$QUADLET_DIR/${path##*/}")" == 644 ]] || die "protected Quadlet mode mismatch"
  done
  for path in "$root/$init_db" "$root/$init_db"/* "$root"/deploy/{postgres,pgbouncer,nginx}/*; do
    actual="$(stat -c %a "$OPT_CURRENT/${path#"$root/"}")"
    expected=644; [[ ! -x "$path" ]] || expected=755
    [[ "$actual" == "$expected" || "$actual" == "$((expected + 20))" ]] ||
      die "protected source mode mismatch: ${path#"$root/"}"
  done
  [[ "$(stat -c %a /run/hololive-bot/pgbouncer/pgbouncer.ini)" == 644 ]] || die "protected pooler mode mismatch"
  state="$(systemctl show --all hololive-db-migrate.service -p LoadState -p ActiveState -p SubState -p Job)" || return 1
  [[ "$(LC_ALL=C sort <<<"$state")" == $'ActiveState=inactive\nJob=\nLoadState=loaded\nSubState=dead' ]] ||
    die "schema migrator is not idle"
  for name in holo-postgres holo-pgbouncer; do
    state="$(podman container inspect --format '{{.Id}} {{.State.Pid}} {{.State.Running}} {{.State.Health.Status}}' "$name")" || return 1
    read -r cid pid running health <<<"$state"
    [[ "$cid" =~ ^[0-9a-f]{64}$ && "$pid" =~ ^[1-9][0-9]*$ && "$running" == true && "$health" == healthy ]] ||
      die "protected container is not healthy: $name"
    printf '%s %s\n' "$name" "$state"
    if [[ "$name" == holo-postgres ]]; then
      binds=("$OPT_CURRENT/$init_db|/docker-entrypoint-initdb.d" "$OPT_CURRENT/deploy/postgres/pg_hba.conf|/run/hololive-bot/postgres/pg_hba.conf")
    else
      binds=(/run/hololive-bot/pgbouncer/pgbouncer.ini\|/etc/pgbouncer/pgbouncer.ini)
      for path in pg_hba.conf stats-users.txt; do binds+=("$OPT_CURRENT/deploy/pgbouncer/$path|/etc/pgbouncer/$path"); done
    fi
    verify_running_binds "$pid" "${binds[@]}" || return 1
  done
}

verify_execution_tree() {
  local verifier="$1" asset path dest unit
  shift
  local -a paths=("$@" "$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
    "$OPT_CURRENT/scripts/ci/postgres-capacity-policy.tsv"
    "$OPT_CURRENT/deploy/pgbouncer/pg_hba.conf"
    "$OPT_CURRENT/deploy/pgbouncer/stats-users.txt"
    "$OPT_CURRENT/deploy/nginx/admin-dashboard-ingress.conf.template"
    "$OPT_CURRENT/deploy/postgres/pg_hba.conf")
  for asset in "${host_assets[@]}"; do
    IFS='|' read -r path dest _ <<<"$asset"
    paths+=("$dest" "$OPT_CURRENT/$path")
  done
  for unit in "$QUADLET_DIR"/* "$QUADLET_DIR"/*.d/*.conf; do
    [[ ! -e "$unit" ]] || paths+=("$unit")
  done
  bash "$verifier" "${paths[@]}" || die "exec-tree ownership verification failed"
}

sync_flags=()
if [[ "$application_only" == true ]]; then
  init_db=hololive/hololive-api/scripts/init-db
  protected=(deploy "$init_db" scripts/systemd scripts/ci/postgres-capacity-policy.tsv)
  deploy_exceptions=("$migrator_path")
  for path in deploy/{quadlet,compose,podman/{iris-seoul,kapu}}/README.md; do
    [[ ! -f "$STAGING/$path" || ! -f "$OPT_CURRENT/$path" ]] || deploy_exceptions+=("$path")
  done
  deploy_filters=()
  for path in "${deploy_exceptions[@]}"; do
    deploy_filters+=("--exclude=/${path#deploy/}")
    [[ -f "$STAGING/$path" && ! -L "$STAGING/$path" && -f "$OPT_CURRENT/$path" && ! -L "$OPT_CURRENT/$path" ]] ||
      die "application-only requires a regular deployment file: $path"
  done
  assert_same_tree "$STAGING/deploy" "$OPT_CURRENT/deploy" "${deploy_filters[@]}"
  assert_same_tree "$STAGING/$init_db" "$OPT_CURRENT/$init_db"
  assert_same_tree "$STAGING/scripts/systemd" "$OPT_CURRENT/scripts/systemd"
  assert_same_file "$STAGING/scripts/ci/postgres-capacity-policy.tsv" "$OPT_CURRENT/scripts/ci/postgres-capacity-policy.tsv"

  verify_paths=("$OPT_CURRENT/.source-revision" "$OPT_CURRENT/$init_db"/*)
  if [[ -n "$generation" ]]; then
    legacy="$(<"$OPT_CURRENT/.source-revision")"
    [[ "$legacy" =~ ^[0-9a-f]{40}$ ]] || die "invalid protected host revision"
    git -C "$REPO_ROOT" merge-base --is-ancestor "$legacy" "$source_base" ||
      die "protected host libexec source is not an installed predecessor"
  fi
  for asset in "${host_assets[@]}"; do
    IFS='|' read -r path dest mode <<<"$asset"
    if [[ -n "$generation" && "$dest" == "$LIBEXEC_DIR/"* ]]; then
      assert_same_file "$OPT_CURRENT/$path" "$dest"
      legacy_blob="$(git -C "$REPO_ROOT" rev-parse --verify "$legacy:$path")" &&
        [[ "$legacy_blob" =~ ^[0-9a-f]{40}$ && "$(git hash-object --no-filters "$dest")" == "$legacy_blob" ]] ||
        die "installed libexec differs from its protected source: $path"
      candidate_blob="$(git -C "$REPO_ROOT" rev-parse --verify "$HEAD_REVISION:$path")" &&
        [[ "$candidate_blob" =~ ^[0-9a-f]{40}$ && -f "$STAGING/$path" && ! -L "$STAGING/$path" &&
          "$(git hash-object --no-filters "$STAGING/$path")" == "$candidate_blob" ]] ||
        die "staged libexec differs from its candidate source: $path"
    else
      assert_same_file "$STAGING/$path" "$OPT_CURRENT/$path"
      assert_same_file "$STAGING/$path" "$dest"
    fi
    [[ "$(stat -c '%u:%g:%a' "$dest")" == "0:0:$mode" ]] || die "application-only installation mode drift: $dest"
    protected+=("$path")
  done
  assert_installed_migrator "$OPT_CURRENT/$migrator_path"
  [[ -f "$OPT_CURRENT/.source-revision" && ! -L "$OPT_CURRENT/.source-revision" ]] ||
    die "application-only requires a deployed source revision"
  # shellcheck source=scripts/deploy/lib/source-revision.sh
  . "$STAGING/scripts/deploy/lib/source-revision.sh"
  deploy_assert_full_revision "$(<"$OPT_CURRENT/.source-revision")"

  links="$(find "$QUADLET_DIR" -type l -print -quit)"
  [[ -z "$links" ]] || die "application-only symlink in Quadlet installation: $links"
  for path in "${protected[@]}"; do
    sync_flags+=("--exclude=/$path")
    verify_paths+=("$OPT_CURRENT/$path")
  done
  for path in "${deploy_exceptions[@]}"; do
    verify_paths+=("$OPT_CURRENT/$path")
  done
  verify_execution_tree "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "${verify_paths[@]}"
  sync_flags+=(--checksum)
fi

# shellcheck source=scripts/deploy/lib/compose-env.sh
. "$STAGING/scripts/deploy/lib/compose-env.sh"
compose_env_validate_file_format "$COMPOSE_ENV"
env_value() {
  if compose_env_key_exists_in_file "$COMPOSE_ENV" "$1"; then
    compose_env_read_value_from_file "$COMPOSE_ENV" "$1"
  fi
}
[[ "$(env_value HOLOLIVE_VALKEY_HA_ENABLED)" == 1 ]] || die "HOLOLIVE_VALKEY_HA_ENABLED=1 is required in $COMPOSE_ENV"
[[ -n "$(env_value VALKEY_SENTINEL_ADDRS)" ]] || die "VALKEY_SENTINEL_ADDRS is required in $COMPOSE_ENV"
OPTIONAL_FEATURES=(
  "x-spaces|HOLOLIVE_X_SPACES_ENABLED|40-x-spaces.conf|hololive-api hololive-alarm-worker"
  "schedule-poster|HOLOLIVE_SCHEDULE_POSTER_ENABLED|41-schedule-poster.conf|hololive-api"
  "schedule-poster-review|HOLOLIVE_SCHEDULE_POSTER_REVIEW_ENABLED|42-schedule-poster-review.conf|hololive-api"
  "broadcast-type-decision|HOLOLIVE_BROADCAST_TYPE_DECISION_ENABLED|43-broadcast-type-decision.conf|hololive-api"
)
declare -A feature_enabled=()
feature_summary=""
for feature in "${OPTIONAL_FEATURES[@]}"; do
  IFS='|' read -r name flag _ _ <<<"$feature"
  value="$(env_value "$flag")"
  case "${value:-0}" in
    0|1) ;;
    *) die "$flag must be 0 or 1" ;;
  esac
  feature_enabled[$name]="${value:-0}"
  feature_summary+="${feature_summary:+ }${name}=${value:-0}"
done
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "$STAGING/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_assert_target "$STAGING" "$COMPOSE_ENV" || die "PostgreSQL connection budget check failed for $COMPOSE_ENV; nothing was installed"

quadlet_check_dir="$QUADLET_CHECK/hololive-bot"
mkdir -p "$quadlet_check_dir"
for dropin_dir in "$QUADLET_DIR"/*.d; do
  [[ -d "$dropin_dir" ]] || continue
  cp -a -- "$dropin_dir" "$quadlet_check_dir/"
done
cp -- "$STAGING"/deploy/quadlet/hololive-bot/* "$quadlet_check_dir/"
for feature in "${OPTIONAL_FEATURES[@]}"; do
  IFS='|' read -r name _ file units <<<"$feature"
  rm -f -- "$quadlet_check_dir"/*.container.d/"$file"
  [[ "${feature_enabled[$name]}" == 1 ]] || continue
  for unit in $units; do
    mkdir -p "$quadlet_check_dir/$unit.container.d"
    cp -- "$STAGING/deploy/quadlet/optional/$name/$unit.container.d/$file" "$quadlet_check_dir/$unit.container.d/"
  done
done
if [[ "$application_only" == true ]]; then
  assert_same_tree "$quadlet_check_dir" "$QUADLET_DIR" "--exclude=/$migrator"
  required_exec=--pool-only=auth
  if [[ -n "$source_base" ]]; then
    required_exec=--schema-only
    assert_installed_migrator "$STAGING/$migrator_path"
    if [[ -n "$generation" ]]; then
      cp -- "$QUADLET_DIR/$migrator" "$quadlet_check_dir/$migrator"
      protected_inventory "$STAGING" >"$QUADLET_CHECK/protected.before"
      if [[ -z "$writer_revision" && "$predecessor" != "$OPT_CURRENT" ]]; then
        classify_predecessor
        if [[ -n "$recovered_from" ]]; then
          verify_snapshot "$source_base" "$predecessor" sealed
          require_completed_recovery
        fi
        accepted_image="$(accepted_migrator "$QUADLET_CHECK/pairs.before")"
      fi
    fi
  fi
  [[ "$(awk -F= '/^\[/{s=$0} s=="[Container]" && $1=="Exec"{print substr($0,6)}' "$QUADLET_DIR/$migrator")" == "$required_exec" ]] ||
    die "installed migrator must use $required_exec"
  generated="$QUADLET_CHECK/generated"
  mkdir "$generated"
  QUADLET_UNIT_DIRS="$quadlet_check_dir" /usr/libexec/podman/quadlet "$generated" ||
    die "staged Quadlet units do not generate; nothing was installed"
  if [[ -n "$writer_revision" ]]; then
    verify_snapshot "$source_base" "$predecessor" sealed
    check_unstarted_source preparing "$QUADLET_CHECK/protected.before" "$QUADLET_CHECK/predecessor.writers" "$QUADLET_CHECK/pairs.before"
  elif [[ -n "$generation" && "$predecessor" != "$OPT_CURRENT" ]]; then
    awk -v image="$accepted_image" -v hook="/usr/bin/bash $predecessor/scripts/deploy/quadlet-prestart.sh hololive-db-migrate schema" '
      /^ExecStart=/ { starts++; if ($(NF-1) != image || $NF != "--check") bad=1 }
      /^ExecStartPre=$/ { pre=0; badpre=0; next }
      /^ExecStartPre=/ { pre++; if ($0 != "ExecStartPre=" hook) badpre=1 }
      END { exit !(starts==1 && pre==1 && !bad && !badpre) }
    ' "$generated/${migrator%.container}.service" || die "installed migrator does not check the active source generation"
  else
    awk '
      /^ExecStart=/ { starts++; if ($(NF-1) !~ /^(\$\{HOLOLIVE_API_IMAGE\}|sha256:[[:xdigit:]]+)$/ || $NF != "--schema-only") bad=1 }
      /^ExecStartPre=/ { if ($0=="ExecStartPre=/usr/local/libexec/hololive-bot/quadlet-prestart hololive-db-migrate schema") schema++; else bad=1 }
      END { exit !(starts==1 && schema==1 && !bad) }
    ' "$generated/${migrator%.container}.service" || die "application-only requires the schema-only migrator"
  fi
else
  QUADLET_UNIT_DIRS="$quadlet_check_dir" /usr/libexec/podman/quadlet -dryrun >/dev/null \
    || die "staged Quadlet units do not generate; nothing was installed"
fi
log "staged Quadlet units generate ($feature_summary)"

if [[ -n "$source_base" ]]; then
  if [[ -z "$generation" || "$predecessor" == "$OPT_CURRENT" ]]; then
    previous="$QUADLET_CHECK/previous"
    mkdir "$previous"
    git -C "$REPO_ROOT" "${archive_config[@]}" archive "$source_base" | tar -x -C "$previous"
  fi
  tree_filters=(--exclude=/.source-revision --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config --exclude=/artifacts/worker-profile)
  if [[ -n "$generation" ]]; then
    publish_generation
    exit 0
  fi
  if [[ -d "$OPT_CURRENT/artifacts/worker-profile" ]]; then
    mkdir -p "$previous/artifacts" "$STAGING/artifacts"
  fi
  assert_same_tree "$previous" "$OPT_CURRENT" "${tree_filters[@]}"
  replacements+=(.source-revision)
  prepared="$QUADLET_CHECK/replacements"
  mkdir "$prepared"
  verify_paths=()
  for path in "${replacements[@]}"; do
    verify_paths+=("$OPT_CURRENT/$path")
    mkdir -p "$prepared/$(dirname "$path")"
    cp --preserve=all "$OPT_CURRENT/$path" "$prepared/$path"
    cat "$STAGING/$path" >"$prepared/$path"
  done
  bash "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "${verify_paths[@]}"
  backup_current
  cmp -s "$backup/.source-revision" "$OPT_CURRENT/.source-revision" || die "source-only backup stamp mismatch"
  assert_same_tree "$previous" "$backup" "${tree_filters[@]}"
  cp --preserve=all "$OPT_CURRENT/.source-revision" "$QUADLET_CHECK/incomplete"
  printf 'incomplete %s -> %s\n' "$source_base" "$HEAD_REVISION" >"$QUADLET_CHECK/incomplete"
  sync -f "$OPT_ROOT"
  mv -T --no-copy "$QUADLET_CHECK/incomplete" "$OPT_CURRENT/.source-revision"
  sync -f "$OPT_ROOT"
  for path in "${replacements[@]}"; do
    if [[ "$path" == .source-revision ]]; then
      assert_same_tree "$STAGING" "$OPT_CURRENT" "${tree_filters[@]}"
      sync -f "$OPT_CURRENT"
    fi
    mv -T --no-copy "$prepared/$path" "$OPT_CURRENT/$path"
  done
  sync -f "$OPT_CURRENT"
  log "source-only published $source_base -> $HEAD_REVISION; host definitions and runtime untouched"
  exit 0
fi

if [[ "$application_only" == true ]]; then
  backup_current
fi

mkdir -p "$OPT_CURRENT"
rsync -a --delete "${sync_flags[@]}" \
  --exclude=/data --exclude=/logs --exclude=/backups --exclude=/runtime-config --exclude=/artifacts/worker-profile \
  "$STAGING/" "$OPT_CURRENT/"

if [[ "$application_only" == true ]]; then
  for path in "${deploy_exceptions[@]}"; do
    rsync -ac "$STAGING/$path" "$OPT_CURRENT/$path"
  done
  install -m0644 -o root -g root "$STAGING/$migrator_path" "$QUADLET_DIR/$migrator"
  verify_execution_tree "$STAGING/scripts/deploy/verify-exec-tree-ownership.sh" "${verify_paths[@]}"
  log "application-only source and schema migrator published (HEAD=$HEAD_SHA); no daemon-reload or service activation"
  exit 0
fi

if [[ "$preserve_runtime" == true ]]; then
  log "synced execution tree (runtime bytes and ownership unchanged)"
else
  for d in data runtime-config; do
    if [[ -d "$REPO_ROOT/$d" ]]; then
      mkdir -p "$OPT_CURRENT/$d"
      rsync -a --ignore-existing "$REPO_ROOT/$d/" "$OPT_CURRENT/$d/"
    fi
  done
  mkdir -p "$OPT_CURRENT/logs" "$OPT_CURRENT/backups"
  log "synced current (runtime data + runtime-config preserved)"

  chown -R root:root "$OPT_CURRENT"
  chmod -R go-w "$OPT_CURRENT"
  if [[ -d "$OPT_CURRENT/runtime-config" ]]; then
    chmod 0755 "$OPT_CURRENT/runtime-config"
  fi
  for d in data logs backups; do
    [[ -d "$OPT_CURRENT/$d" ]] && chown -R 1000:1000 "$OPT_CURRENT/$d"
  done
  chown root:root "$OPT_ROOT" "$(dirname "$OPT_ROOT")"
  log "ownership normalized (exec tree root, runtime data uid 1000)"
fi

install -d -m0755 -o root -g root "$LIBEXEC_DIR" "$LIBEXEC_DIR/lib"
mkdir -p /etc/nftables.d
for asset in "${host_assets[@]}"; do
  IFS='|' read -r path dest mode <<<"$asset"
  install -m"$mode" -o root -g root "$STAGING/$path" "$dest"
done
log "prestart and admin-dashboard ingress firewall installed"

install -d -m0755 -o root -g root "$QUADLET_DIR"
for unit in "$QUADLET_DIR"/*.container "$QUADLET_DIR"/*.volume "$QUADLET_DIR"/*.network; do
  [[ -e "$unit" ]] || continue
  [[ -e "$STAGING/deploy/quadlet/hololive-bot/${unit##*/}" ]] || rm -f -- "$unit"
done
install -m0644 -o root -g root "$STAGING"/deploy/quadlet/hololive-bot/* "$QUADLET_DIR/"
for feature in "${OPTIONAL_FEATURES[@]}"; do
  IFS='|' read -r name _ file units <<<"$feature"
  for unit in $units; do
    dropin="$QUADLET_DIR/$unit.container.d/$file"
    if [[ "${feature_enabled[$name]}" == 1 ]]; then
      install -d -m0755 -o root -g root "${dropin%/*}"
      install -m0644 -o root -g root "$STAGING/deploy/quadlet/optional/$name/$unit.container.d/$file" "$dropin"
    else
      rm -f -- "$dropin"
    fi
  done
done
log "Quadlet units installed -> $QUADLET_DIR ($feature_summary)"

systemctl daemon-reload
log "daemon-reload done"

verify_execution_tree "$OPT_CURRENT/scripts/deploy/verify-exec-tree-ownership.sh"
log "exec-tree ownership verified"

log "SSOT sync complete (HEAD=$HEAD_SHA)"
log "next: api/alarm-worker release -> sudo $OPT_CURRENT/scripts/deploy/quadlet-redeploy-service.sh <service> <image> [<migrator-image>]; collector c -> po-central-cutover.sh c (from kapu); other unit changes apply on an approved restart"
