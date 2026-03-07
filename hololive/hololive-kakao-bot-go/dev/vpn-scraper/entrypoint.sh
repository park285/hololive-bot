#!/usr/bin/env bash
# Copyright (c) 2025 Kapu
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

set -euo pipefail

# ── 환경변수 검증 ──

require_env() {
  local key="$1"
  if [[ -z "${!key:-}" ]]; then
    echo "missing required env: ${key}" >&2
    exit 1
  fi
}

if [[ -z "${OPENVPN_CONFIG_1:-}" && -n "${OPENVPN_CONFIG:-}" ]]; then
  OPENVPN_CONFIG_1="${OPENVPN_CONFIG}"
fi

require_env "OPENVPN_CONFIG_1"
require_env "NORDVPN_USERNAME"
require_env "NORDVPN_PASSWORD"

DUAL_VPN="${DUAL_VPN:-1}"
VPN_KILLSWITCH="${VPN_KILLSWITCH:-1}"
MAX_RESTART_ATTEMPTS=3

if [[ ! -f "${OPENVPN_CONFIG_1}" ]]; then
  echo "OPENVPN_CONFIG_1 file not found: ${OPENVPN_CONFIG_1}" >&2
  exit 1
fi

if [[ "${DUAL_VPN}" == "1" ]]; then
  if [[ -z "${OPENVPN_CONFIG_2:-}" ]]; then
    echo "DUAL_VPN=1 but OPENVPN_CONFIG_2 not set; falling back to single mode" >&2
    DUAL_VPN=0
  elif [[ ! -f "${OPENVPN_CONFIG_2}" ]]; then
    echo "OPENVPN_CONFIG_2 file not found: ${OPENVPN_CONFIG_2}; falling back to single mode" >&2
    DUAL_VPN=0
  fi
fi

echo "mode: $([ "${DUAL_VPN}" == "1" ] && echo "dual-tunnel ECMP" || echo "single-tunnel")"

# ── 인증 파일 생성 ──

AUTH_FILE="/run/openvpn-auth.txt"
mkdir -p /run
printf "%s\n%s\n" "${NORDVPN_USERNAME}" "${NORDVPN_PASSWORD}" > "${AUTH_FILE}"
chmod 600 "${AUTH_FILE}"

# eth0 기본 게이트웨이 조기 캡처 (ECMP로 default route 교체 전에 저장)
ETH0_GATEWAY="$(ip -4 route show default | awk '/via/{print $3}' | head -n 1 || true)"
echo "eth0 gateway: ${ETH0_GATEWAY:-none}"

# ── ovpn 파싱 함수 ──

# parse_ovpn_config <config_path> <prefix>
# 결과를 {prefix}_host, {prefix}_port, {prefix}_proto 등으로 설정
parse_ovpn_config() {
  local config_path="$1"
  local prefix="$2"

  local _host="" _port="" _proto="" _cipher="" _data_ciphers="" _data_ciphers_fb=""

  local remote_line
  remote_line="$(grep -E '^remote ' "${config_path}" | head -n 1 || true)"
  if [[ -n "${remote_line}" ]]; then
    # shellcheck disable=SC2206
    local parts=(${remote_line})
    if [[ "${#parts[@]}" -ge 3 ]]; then
      _host="${parts[1]}"
      _port="${parts[2]}"
    fi
  fi

  local proto_line
  proto_line="$(grep -E '^proto ' "${config_path}" | head -n 1 || true)"
  if [[ -n "${proto_line}" ]]; then
    # shellcheck disable=SC2206
    local parts=(${proto_line})
    if [[ "${#parts[@]}" -ge 2 ]]; then
      _proto="${parts[1]}"
    fi
  fi

  local data_ciphers_line
  data_ciphers_line="$(grep -E '^data-ciphers ' "${config_path}" | head -n 1 || true)"
  if [[ -n "${data_ciphers_line}" ]]; then
    _data_ciphers="${data_ciphers_line#data-ciphers }"
  fi

  local data_ciphers_fb_line
  data_ciphers_fb_line="$(grep -E '^data-ciphers-fallback ' "${config_path}" | head -n 1 || true)"
  if [[ -n "${data_ciphers_fb_line}" ]]; then
    _data_ciphers_fb="${data_ciphers_fb_line#data-ciphers-fallback }"
  fi

  local cipher_line
  cipher_line="$(grep -E '^cipher ' "${config_path}" | head -n 1 || true)"
  if [[ -n "${cipher_line}" ]]; then
    # shellcheck disable=SC2206
    local parts=(${cipher_line})
    if [[ "${#parts[@]}" -ge 2 ]]; then
      _cipher="${parts[1]}"
    fi
  fi

  # 동적 변수 할당 (eval 금지)
  case "${prefix}" in
    vpn1)
      printf -v vpn1_host '%s' "${_host}"
      printf -v vpn1_port '%s' "${_port}"
      printf -v vpn1_proto '%s' "${_proto}"
      printf -v vpn1_cipher '%s' "${_cipher}"
      printf -v vpn1_data_ciphers '%s' "${_data_ciphers}"
      printf -v vpn1_data_ciphers_fb '%s' "${_data_ciphers_fb}"
      ;;
    vpn2)
      printf -v vpn2_host '%s' "${_host}"
      printf -v vpn2_port '%s' "${_port}"
      printf -v vpn2_proto '%s' "${_proto}"
      printf -v vpn2_cipher '%s' "${_cipher}"
      printf -v vpn2_data_ciphers '%s' "${_data_ciphers}"
      printf -v vpn2_data_ciphers_fb '%s' "${_data_ciphers_fb}"
      ;;
    *)
      echo "unsupported ovpn parse prefix: ${prefix}" >&2
      return 1
      ;;
  esac
}

parse_ovpn_config "${OPENVPN_CONFIG_1}" "vpn1"

if [[ "${DUAL_VPN}" == "1" ]]; then
  parse_ovpn_config "${OPENVPN_CONFIG_2}" "vpn2"
fi

# ── Killswitch ──

setup_killswitch() {
  if [[ "${VPN_KILLSWITCH}" != "1" ]]; then
    echo "killswitch disabled (VPN_KILLSWITCH=${VPN_KILLSWITCH})"
    return 0
  fi

  if [[ -z "${vpn1_host}" || -z "${vpn1_port}" || -z "${vpn1_proto}" ]]; then
    echo "killswitch skipped: failed to parse remote/proto from ovpn1" >&2
    return 0
  fi

  # 컨테이너 재시작 시 규칙 누적 방지
  iptables -F OUTPUT || true
  iptables -P OUTPUT DROP
  iptables -A OUTPUT -o lo -j ACCEPT
  iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT

  # VPN 서버 1 → eth0 허용
  iptables -A OUTPUT -o eth0 -p "${vpn1_proto}" -d "${vpn1_host}" --dport "${vpn1_port}" -j ACCEPT

  # VPN 서버 2 → eth0 허용 (듀얼 모드)
  if [[ "${DUAL_VPN}" == "1" && -n "${vpn2_host:-}" && -n "${vpn2_port:-}" && -n "${vpn2_proto:-}" ]]; then
    iptables -A OUTPUT -o eth0 -p "${vpn2_proto}" -d "${vpn2_host}" --dport "${vpn2_port}" -j ACCEPT
  fi

  # Docker 내부 통신 허용 (eth0 CIDR)
  local eth0_cidr=""
  eth0_cidr="$(ip -o -4 addr show dev eth0 | awk '{print $4}' | head -n 1 || true)"
  if [[ -n "${eth0_cidr}" ]]; then
    iptables -A OUTPUT -o eth0 -d "${eth0_cidr}" -j ACCEPT
  else
    echo "killswitch warning: failed to detect eth0 CIDR; internal traffic may be blocked" >&2
  fi
}

allow_tun_output() {
  if [[ "${VPN_KILLSWITCH}" != "1" ]]; then
    return 0
  fi
  local dev="$1"
  iptables -A OUTPUT -o "${dev}" -j ACCEPT
}

setup_killswitch

# ── OpenVPN 추가 인자 빌드 ──

build_openvpn_extra_args() {
  local cipher="$1"
  local data_ciphers="$2"
  local data_ciphers_fb="$3"
  local -n _result=$4

  _result=()

  # OpenVPN 2.6+: cipher가 data-ciphers에 없으면 경고
  if [[ -n "${cipher}" ]]; then
    local effective="${data_ciphers:-AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305}"
    if ! echo ":${effective}:" | grep -Fq ":${cipher}:"; then
      effective="${effective}:${cipher}"
    fi
    _result+=(--data-ciphers "${effective}")

    if [[ -z "${data_ciphers_fb}" ]]; then
      _result+=(--data-ciphers-fallback "${cipher}")
    fi
  fi
}

# ── OpenVPN 프로세스 시작 ──

start_openvpn() {
  local config="$1"
  local dev="$2"
  local cipher="$3"
  local data_ciphers="$4"
  local data_ciphers_fb="$5"
  local use_route_nopull="$6"

  local extra_args=()
  build_openvpn_extra_args "${cipher}" "${data_ciphers}" "${data_ciphers_fb}" extra_args

  local route_args=()
  if [[ "${use_route_nopull}" == "1" ]]; then
    route_args=(--route-nopull)
  fi

  # stdout → stderr: $() command substitution이 openvpn 출력을 캡처하지 않도록
  openvpn \
    --config "${config}" \
    --dev "${dev}" \
    --auth-user-pass "${AUTH_FILE}" \
    "${extra_args[@]}" \
    "${route_args[@]}" \
    --verb 3 >&2 &
  echo $!
}

wait_for_tun() {
  local dev="$1"
  local pid="$2"
  local timeout="${3:-60}"

  echo "waiting for ${dev}..."
  for _ in $(seq 1 "${timeout}"); do
    if ip link show "${dev}" >/dev/null 2>&1; then
      echo "${dev} is up"
      return 0
    fi
    if ! kill -0 "${pid}" >/dev/null 2>&1; then
      echo "openvpn exited before ${dev} became ready" >&2
      wait "${pid}" || true
      return 1
    fi
    sleep 1
  done

  echo "${dev} not found after ${timeout}s; openvpn may have failed" >&2
  return 1
}

# tun0 시작
openvpn_pid_1=$(start_openvpn \
  "${OPENVPN_CONFIG_1}" "tun0" \
  "${vpn1_cipher}" "${vpn1_data_ciphers}" "${vpn1_data_ciphers_fb}" \
  "${DUAL_VPN}")

# tun1 시작 (듀얼 모드)
openvpn_pid_2=""
if [[ "${DUAL_VPN}" == "1" ]]; then
  openvpn_pid_2=$(start_openvpn \
    "${OPENVPN_CONFIG_2}" "tun1" \
    "${vpn2_cipher}" "${vpn2_data_ciphers}" "${vpn2_data_ciphers_fb}" \
    "1")
fi

cleanup() {
  kill "${openvpn_pid_1}" >/dev/null 2>&1 || true
  [[ -n "${openvpn_pid_2}" ]] && kill "${openvpn_pid_2}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# tun0 대기 + killswitch 허용
if ! wait_for_tun "tun0" "${openvpn_pid_1}"; then
  exit 1
fi
allow_tun_output "tun0"

# tun1 대기 + killswitch 허용 (듀얼 모드)
if [[ "${DUAL_VPN}" == "1" ]]; then
  if ! wait_for_tun "tun1" "${openvpn_pid_2}"; then
    echo "tun1 failed; falling back to single-tunnel mode" >&2
    DUAL_VPN=0
    openvpn_pid_2=""
  else
    allow_tun_output "tun1"
  fi
fi

# ── DNS 설정 (듀얼 모드: route-nopull로 push DNS 불가) ──

if [[ "${DUAL_VPN}" == "1" ]]; then
  echo "setting DNS manually (route-nopull mode)"
  : > /etc/resolv.conf
  echo "nameserver 127.0.0.11" >> /etc/resolv.conf  # Docker 내부 DNS (컨테이너 hostname 해석)
  echo "nameserver 103.86.96.100" >> /etc/resolv.conf  # NordVPN (외부 DNS fallback)
  echo "nameserver 103.86.99.100" >> /etc/resolv.conf  # NordVPN (외부 DNS fallback)
fi

# ── ECMP 라우팅 (듀얼 모드) ──

get_tun_gateway() {
  local dev="$1"
  local gw=""

  # 1. point-to-point 피어 주소
  gw=$(ip -4 addr show dev "${dev}" | awk '/peer/{print $4}' | sed 's|/.*||' || true)
  if [[ -n "${gw}" ]]; then echo "${gw}"; return; fi

  # 2. 라우팅 테이블의 via
  gw=$(ip -4 route show dev "${dev}" | awk '/via/{print $3}' | head -n 1 || true)
  if [[ -n "${gw}" ]]; then echo "${gw}"; return; fi

  # 3. subnet topology: IP/CIDR에서 네트워크 주소 + 1 = 게이트웨이
  #    예: 10.100.0.2/20 → network=10.100.0.0 → gw=10.100.0.1
  local ip_cidr
  ip_cidr=$(ip -4 -o addr show dev "${dev}" | awk '{print $4}')
  if [[ -n "${ip_cidr}" ]]; then
    local ip="${ip_cidr%%/*}"
    local prefix="${ip_cidr##*/}"
    local IFS='.'
    # shellcheck disable=SC2206
    local -a octets=(${ip})
    local ip_int=$(( (octets[0] << 24) + (octets[1] << 16) + (octets[2] << 8) + octets[3] ))
    local mask=$(( 0xFFFFFFFF << (32 - prefix) & 0xFFFFFFFF ))
    local network=$(( ip_int & mask ))
    local gw_int=$(( network + 1 ))
    gw="$(( (gw_int >> 24) & 0xFF )).$(( (gw_int >> 16) & 0xFF )).$(( (gw_int >> 8) & 0xFF )).$(( gw_int & 0xFF ))"
    echo "${gw}"
  fi
}

setup_ecmp() {
  local gw0 gw1
  gw0=$(get_tun_gateway "tun0")
  gw1=$(get_tun_gateway "tun1")

  if [[ -z "${gw0}" || -z "${gw1}" ]]; then
    echo "ECMP setup failed: could not determine gateways (gw0=${gw0:-none}, gw1=${gw1:-none})" >&2
    echo "falling back to single default route via tun0" >&2
    ip route del default 2>/dev/null || true
    ip route add default dev tun0
    return 1
  fi

  echo "setting up ECMP: tun0 via ${gw0}, tun1 via ${gw1}"
  ip route del default 2>/dev/null || true
  ip route add default \
    nexthop via "${gw0}" dev tun0 weight 1 \
    nexthop via "${gw1}" dev tun1 weight 1
}

setup_single_route() {
  local dev="$1"
  local gw
  gw=$(get_tun_gateway "${dev}")
  ip route del default 2>/dev/null || true
  if [[ -n "${gw}" ]]; then
    ip route add default via "${gw}" dev "${dev}"
  else
    ip route add default dev "${dev}"
  fi
}

if [[ "${DUAL_VPN}" == "1" ]]; then
  # route-nopull 사용 시 OpenVPN이 추가하는 host route도 빠짐
  # VPN 서버 IP → eth0 게이트웨이로 직접 라우팅 (라우팅 루프 방지)
  if [[ -n "${ETH0_GATEWAY}" ]]; then
    echo "adding host routes for VPN servers via eth0 (gw=${ETH0_GATEWAY})"
    ip route add "${vpn1_host}/32" via "${ETH0_GATEWAY}" dev eth0 2>/dev/null || true
    ip route add "${vpn2_host}/32" via "${ETH0_GATEWAY}" dev eth0 2>/dev/null || true
  else
    echo "WARNING: could not determine eth0 gateway; VPN server routes not added" >&2
  fi

  # ECMP 수동 설정
  setup_ecmp
else
  # 단일 모드: OpenVPN이 라우트를 push하므로 추가 설정 불필요
  :
fi

# ── sockd.conf 동적 생성 ──

generate_sockd_conf() {
  local conf="/etc/sockd.conf"

  cat > "${conf}" <<'SOCKD_HEADER'
logoutput: stdout

# Listen on all interfaces inside the container; published only to host loopback by docker-compose.
internal: 0.0.0.0 port = 1080

SOCKD_HEADER

  if [[ "${DUAL_VPN}" == "1" ]]; then
    # 듀얼 모드: 두 터널 인터페이스를 external로 등록
    # external.rotation: route → Dante가 라우팅 기반 분산
    cat >> "${conf}" <<'SOCKD_DUAL'
external.rotation: route
external: tun0
external: tun1

SOCKD_DUAL
  else
    cat >> "${conf}" <<'SOCKD_SINGLE'
# Force outbound connections to use the VPN interface (created by OpenVPN).
external: tun0

SOCKD_SINGLE
  fi

  cat >> "${conf}" <<'SOCKD_RULES'
socksmethod: none

client pass {
  from: 0.0.0.0/0 to: 0.0.0.0/0
  log: error
}

socks pass {
  from: 0.0.0.0/0 to: 0.0.0.0/0
  command: connect
  log: error
}
SOCKD_RULES
}

generate_sockd_conf

echo "starting sockd (dante) on 0.0.0.0:1080"
sockd -f /etc/sockd.conf -N 1 &
socks_pid=$!
sleep 1

# external.rotation 미지원 시 fallback: rotation 제거 후 재시도
if ! kill -0 "${socks_pid}" >/dev/null 2>&1 && [[ "${DUAL_VPN}" == "1" ]]; then
  echo "sockd failed; retrying without external.rotation" >&2
  sed -i '/^external\.rotation/d' /etc/sockd.conf
  sockd -f /etc/sockd.conf -N 1 &
  socks_pid=$!
  sleep 1
  if ! kill -0 "${socks_pid}" >/dev/null 2>&1; then
    echo "sockd failed even without rotation; falling back to tun0 only" >&2
    # tun0 단독 external로 재생성
    DUAL_VPN=0
    generate_sockd_conf
    sockd -f /etc/sockd.conf -N 1 &
    socks_pid=$!
  fi
fi

# ── 감시 루프 ──

restart_count_1=0
restart_count_2=0

restart_tunnel() {
  local tunnel_num="$1"  # 1 또는 2
  local dev="tun$((tunnel_num - 1))"
  local config_var="OPENVPN_CONFIG_${tunnel_num}"
  local config="${!config_var}"
  local cipher_var="vpn${tunnel_num}_cipher"
  local dc_var="vpn${tunnel_num}_data_ciphers"
  local dcfb_var="vpn${tunnel_num}_data_ciphers_fb"

  echo "restarting tunnel ${tunnel_num} (${dev})..."
  local new_pid
  new_pid=$(start_openvpn \
    "${config}" "${dev}" \
    "${!cipher_var}" "${!dc_var}" "${!dcfb_var}" \
    "1")

  if wait_for_tun "${dev}" "${new_pid}" 30; then
    allow_tun_output "${dev}"
    echo "tunnel ${tunnel_num} restarted successfully (pid=${new_pid})"

    if [[ "${tunnel_num}" == "1" ]]; then
      openvpn_pid_1="${new_pid}"
    else
      openvpn_pid_2="${new_pid}"
    fi

    # ECMP 복원
    setup_ecmp
    return 0
  else
    echo "tunnel ${tunnel_num} restart failed" >&2
    kill "${new_pid}" >/dev/null 2>&1 || true
    return 1
  fi
}

degrade_to_single() {
  local surviving_dev="$1"
  echo "degrading to single-tunnel mode via ${surviving_dev}"
  setup_single_route "${surviving_dev}"
}

while true; do
  # sockd 감시
  if ! kill -0 "${socks_pid}" >/dev/null 2>&1; then
    echo "sockd exited; stopping all tunnels" >&2
    kill "${openvpn_pid_1}" >/dev/null 2>&1 || true
    [[ -n "${openvpn_pid_2}" ]] && kill "${openvpn_pid_2}" >/dev/null 2>&1 || true
    exit 1
  fi

  # tun0 감시
  if ! kill -0 "${openvpn_pid_1}" >/dev/null 2>&1; then
    echo "tun0 (openvpn_pid_1=${openvpn_pid_1}) exited" >&2

    if [[ "${DUAL_VPN}" == "1" ]]; then
      # tun1이 살아있으면 단독 라우팅 전환
      if [[ -n "${openvpn_pid_2}" ]] && kill -0 "${openvpn_pid_2}" >/dev/null 2>&1; then
        degrade_to_single "tun1"

        restart_count_1=$((restart_count_1 + 1))
        if [[ "${restart_count_1}" -le "${MAX_RESTART_ATTEMPTS}" ]]; then
          echo "attempting tun0 restart (${restart_count_1}/${MAX_RESTART_ATTEMPTS})"
          if restart_tunnel 1; then
            restart_count_1=0
          fi
        else
          echo "tun0 exceeded max restart attempts; running on tun1 only" >&2
        fi
      else
        echo "both tunnels down; exiting" >&2
        kill "${socks_pid}" >/dev/null 2>&1 || true
        exit 1
      fi
    else
      # 단일 모드: 터널 장애 → 컨테이너 종료
      echo "single-mode tunnel down; exiting" >&2
      kill "${socks_pid}" >/dev/null 2>&1 || true
      exit 1
    fi
  fi

  # tun1 감시 (듀얼 모드)
  if [[ "${DUAL_VPN}" == "1" && -n "${openvpn_pid_2}" ]]; then
    if ! kill -0 "${openvpn_pid_2}" >/dev/null 2>&1; then
      echo "tun1 (openvpn_pid_2=${openvpn_pid_2}) exited" >&2

      # tun0이 살아있으면 단독 라우팅 전환
      if kill -0 "${openvpn_pid_1}" >/dev/null 2>&1; then
        degrade_to_single "tun0"

        restart_count_2=$((restart_count_2 + 1))
        if [[ "${restart_count_2}" -le "${MAX_RESTART_ATTEMPTS}" ]]; then
          echo "attempting tun1 restart (${restart_count_2}/${MAX_RESTART_ATTEMPTS})"
          if restart_tunnel 2; then
            restart_count_2=0
          fi
        else
          echo "tun1 exceeded max restart attempts; running on tun0 only" >&2
        fi
      else
        echo "both tunnels down; exiting" >&2
        kill "${socks_pid}" >/dev/null 2>&1 || true
        exit 1
      fi
    fi
  fi

  sleep 2
done
