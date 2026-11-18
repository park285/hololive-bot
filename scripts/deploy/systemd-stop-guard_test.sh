#!/usr/bin/env bash
set -euo pipefail
script=$(realpath -- "${BASH_SOURCE[0]}")
if [[ $# == 0 ]]; then
  [[ $EUID != 0 && $(hostname -s) == kapu ]] || { echo 'kapu에서 일반 사용자로 실행하세요.' >&2; exit 2; }
  name="holo-guard-$(cat /proc/sys/kernel/random/uuid)"
  status=0
  sudo -n systemd-run --quiet --wait --collect --unit="$name.service" -p "User=$(id -un)" \
    -p "RuntimeDirectory=$name" -p RuntimeDirectoryMode=0700 -p Delegate=yes -p RuntimeMaxSec=65s \
    -p TimeoutStopSec=4s -p KillMode=control-group -p NoNewPrivileges=yes -p PrivateTmp=yes -p StandardOutput=journal \
    -p StandardError=journal /usr/bin/bash "$script" --private || status=$?
  sudo -n journalctl --boot -q --no-pager -o cat -u "$name.service"
  exit "$status"
fi
[[ $# == 1 && $1 == --private && $EUID != 0 && ${RUNTIME_DIRECTORY:-} =~ ^/run/holo-guard-[0-9a-f-]{36}$ ]]
[[ $(stat -c '%u:%a' -- "$RUNTIME_DIRECTORY") == "$EUID:700" ]]
r=$(mktemp -d /tmp/holo-guard.XXXXXXXX)
umask 077
export HOLOLIVE_QUADLET_TEST_ROOT="$r" HOME="$r/home"
system=$r/etc/systemd/system
mkdir -p "$system" "$r/units" "$HOME"
export SYSTEMD_UNIT_PATH="$system:$r/units" SYSTEMD_GENERATOR_PATH=/dev/null SYSTEMD_ENVIRONMENT_GENERATOR_PATH=/dev/null
unset NOTIFY_SOCKET
helper="${script%/*}/lib/systemd-stop-guard.py"
po=hololive-youtube-po-d.service
collector=hololive-youtube-collector-d.service
foreign=hololive-api.service
reset=hololive-alarm-worker.service
drift=hololive-youtube-po-b.service
manager=''
private_ctl() { DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/systemd/private" systemctl --user "$@"; }
close_manager() {
  if [[ -n $manager ]]; then
    private_ctl stop dbus.socket dbus.service >/dev/null 2>&1 || :
    kill -KILL "$manager" 2>/dev/null || :
    wait "$manager" 2>/dev/null || :
    manager=''
  fi
}
trap close_manager EXIT
trap 'echo "isolated guard test failed at line $LINENO" >&2' ERR
new_manager() {
  export XDG_RUNTIME_DIR="$r/$1" DBUS_SESSION_BUS_ADDRESS="unix:path=$r/$1/bus"
  mkdir "$XDG_RUNTIME_DIR"
  /usr/lib/systemd/systemd --user --unit=empty.target --log-target=console >"$r/$1.log" 2>&1 & manager=$!
  for _ in {1..100}; do
    [[ ! -S $XDG_RUNTIME_DIR/systemd/private ]] || break
    kill -0 "$manager" || return 1
    sleep 0.03
  done
  [[ -S $XDG_RUNTIME_DIR/systemd/private ]]
  local paths
  paths=$(private_ctl show -p UnitPath --value)
  [[ " $paths " == *" $system "* && " $paths " == *" $r/units "* ]]
  private_ctl start dbus.socket dbus.service
  busctl --user --timeout=4 get-property org.freedesktop.systemd1 /org/freedesktop/systemd1 \
    org.freedesktop.systemd1.Manager Version >/dev/null
}
prop() { systemctl --user show "$1" -p "$2" --value; }
anchor() { printf '%s/.%s.hold.mask' "$system" "${1%.service}"; }
guard() {
  local a
  a=$(anchor "$2")
  python3 -I -B "$helper" "$1" "$2" "$a" "$(stat -c '%d:%i' -- "$a")"
}
reject() { if "$@"; then echo "unexpected success: $1" >&2; exit 1; fi; }
cat >"$r/bus.conf" <<'XML'
<busconfig><type>session</type><policy context="default"><allow send_destination="*"/><allow receive_sender="*"/><allow own="org.freedesktop.systemd1"/></policy></busconfig>
XML
printf '[Unit]\nDefaultDependencies=no\n[Socket]\nListenStream=%%t/bus\n' >"$r/units/dbus.socket"
printf '[Unit]\nDefaultDependencies=no\nAfter=dbus.socket\nRequires=dbus.socket\n[Service]\nType=notify\nSockets=dbus.socket\nExecStart=/usr/bin/dbus-broker-launch --scope user --config-file %s/bus.conf\n' "$r" >"$r/units/dbus.service"
cat >"$r/worker.py" <<'PY'
import os
from pathlib import Path
import signal
import socket
import sys
record = Path(sys.argv[1])
def append(event):
    with record.open('a') as out:
        print(event, file=out)
def finish(*_):
    append('drained')
    sys.exit(0)
signal.signal(signal.SIGTERM, finish)
append('started')
with socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM) as notify:
    notify.sendto(b'READY=1', os.environ['NOTIFY_SOCKET'].replace('@', '\0', 1))
while True:
    signal.pause()
PY
cat >"$r/stop" <<'SH'
printf 'original-ExecStop\n' >>"$1"
kill -TERM "$2"
SH
printf '[Unit]\nDefaultDependencies=no\n' >"$r/units/empty.target"
for u in "$collector" "$po" "$foreign" "$reset" "$drift"; do
  {
    printf '[Unit]\nDefaultDependencies=no\n'
    if [[ $u == "$collector" ]]; then printf 'Requires=%s\nAfter=%s\n' "$po" "$po"; fi
    printf '[Service]\nType=notify\nKillMode=mixed\nRestart=always\nExecStart=/usr/bin/python3 -I -B %s/worker.py %s/%s.events\nExecStop=/usr/bin/bash %s/stop %s/%s.events $MAINPID\n' "$r" "$r" "$u" "$r" "$r" "$u"
  } >"$r/units/$u"
  ln -s /dev/null "$(anchor "$u")"
done
printf '[Unit]\nDefaultDependencies=no\nWants=%s %s\nAfter=%s %s\n' "$collector" "$po" "$collector" "$po" >"$r/units/consumer.target"
new_manager first
systemctl --user start "$collector" "$foreign" "$reset" "$drift"
mkdir "$system/$foreign.d" "$system/$reset.d"
printf '# operator\n' >"$system/$foreign.d/90-hololive-stop-guard.conf"
reject guard arm "$foreign"
[[ $(cat "$system/$foreign.d/90-hololive-stop-guard.conf") == '# operator' && ! -e $system/$foreign ]]
printf '[Unit]\nConditionPathExists=\n' >"$system/$reset.d/99-reset.conf"
reject guard arm "$reset"
[[ ! -L $system/$reset && -f $system/$reset.d/90-hololive-stop-guard.conf &&
   $(prop "$reset" ActiveState) == active ]]
systemctl --user stop "$reset" "$foreign"
guard arm "$drift"
printf '[Service]\nKillMode=control-group\n' >"$system/$drift.d/99-change-stop.conf"
systemctl --user daemon-reload
reject guard check "$drift"
[[ $(prop "$drift" ActiveState) == active && ! -L $system/$drift ]]
systemctl --user stop "$drift"
guard arm "$po"
guard arm "$collector"
guard check "$po"
reject guard seal "$collector"
collector_inv=$(prop "$collector" InvocationID)
systemctl --user stop --job-mode=ignore-dependencies "$po"
[[ $(prop "$collector" ActiveState) == active && $(prop "$collector" InvocationID) == "$collector_inv" ]]
systemctl --user stop "$collector"
normal=$'started\noriginal-ExecStop\ndrained'
for u in "$po" "$collector"; do [[ $(cat "$r/$u.events") == "$normal" ]]; done
systemctl --user start "$po" consumer.target
for u in "$po" "$collector"; do [[ $(prop "$u" ActiveState) == inactive && $(cat "$r/$u.events") == "$normal" ]]; done
echo 'PASS: original stop/drain; explicit, dependency and automatic reactivation blocked'
close_manager
new_manager fresh
systemctl --user start consumer.target
for u in "$po" "$collector"; do
  [[ $(prop "$u" ConditionResult) == no && $(cat "$r/$u.events") == "$normal" ]]
  guard seal "$u"
  [[ $(prop "$u" LoadState) == masked && $(stat -c '%d:%i' "$system/$u") == "$(stat -c '%d:%i' "$(anchor "$u")")" && ! -e $system/$u.d ]]
  reject systemctl --user start "$u"
done
echo 'PASS: persistent guard on fresh manager, final owned mask, foreign and ineffective guard rejected'
