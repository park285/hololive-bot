# 고정 인프라 이미지의 취약점 도달 조건 재검토

> 역사 기록입니다. 현재 이미지는 [예외 없는 이미지 빌드](image-remediation-20260921.md)를 따르며, 아래 예외 YAML과 checker는 제거됐습니다. 이 문서는 현재 취약점 허용 근거가 아닙니다.

2026-09-14에 기존 예외가 만료되어 관리 웹 발행 검사가 멈췄다. 이미지나 운영 의존성을
변경하지 않고 기존 정확한 ARM64 digest의 보고를 다시 검사했다. 이 문서는 2026-09-21 UTC까지의
명명된 오탐 판단 근거이며, 제품 전체의 무취약성 보고가 아니다. 소유자는 hololive-bot이다.
고정 source/image 또는 아래 실행 조건이 바뀌면 예외를 사용할 수 없다. upstream의 수정 이미지가
나오면 별도 검증 후 예외를 제거하며, 그 전에도 만료 시 재검토 없이는 발행 검사가 실패한다.

## 검사와 해석

Trivy 0.74.0의 갱신한 DB로 `run-final-image-scan.sh`에 적힌 정확한 remote digest를
`--platform linux/arm64 --image-src remote --scanners vuln`로 검사했다. HIGH/CRITICAL의
고유 CVE는 Nginx 1, PostgreSQL 30, deunhealth 28, socket-proxy 8개다.
기존 PostgreSQL 예외 23개에 libuuid의 util-linux 도구 관련 보고 7개가 추가됐다.
허용 tuple은 각 YAML과 `check-trivyignore-contract.py`의 CVE·package version·statement·expiry에 한정된다.
그 밖의 보고는 기존 severity gate에서 계속 실패한다.

govulncheck 1.8.0은 정확한 upstream source와 **이미지의 Go 버전**으로 실행했다.
deunhealth `37e5bd45036a29867fa71e68db3975c0b971c708`/Go 1.25.5와
socket-proxy `bcb95c8f067dfb62f70f2cdfc00154b175e9e4e9`/Go 1.26.5에서
각각 symbol 보고 20개, 5개로 exit 3이었다. 이전의 socket-proxy 'govulncheck 통과' 설명은
사용하지 않는다. 호출 그래프는 아래 실제 transport·입력 조건을 모두 모델링하지 않으므로
symbol 도달과 취약한 실행 조건의 도달을 구분한다. 이미지 빌드 도구를 최신 Go로 바꿔 검사하지 않았다.

```sh
GOWORK=off GOTOOLCHAIN=go1.25.5 GOOS=linux GOARCH=arm64 govulncheck ./...
GOWORK=off GOTOOLCHAIN=go1.26.5 GOOS=linux GOARCH=arm64 govulncheck ./...
```

## Nginx와 PostgreSQL

- CVE-2026-14456: Nginx는 고정 ingress 파일의 HTTP listener만 사용한다. PostgreSQL도
  OpenSSL QUIC 서버를 만들지 않는다. [OpenSSL 설명](https://www.openssl-library.org/news/vulnerabilities-3.6/)의
  QUIC-server 조건이 없다. TLS·QUIC·include directive를 추가하면 기존 checker가 거절한다.
- PostgreSQL의 Go stdlib 보고 22개는 `usr/local/bin/gosu`에 속한다. prod와 standby는
  `user: "999:999"`로 시작하므로 이미지 entrypoint의 root 전용 gosu 실행 분기에 들어가지 않는다.
  앱이 이 Go 바이너리로 네트워크 요청·인증서를 처리하지 않는다. 이 판단은 임의의 root 컨테이너에
  동일 이미지를 사용하는 경우에는 적용하지 않는다.
- CVE-2026-53612/53613/53614/76642/78409/78410은 util-linux mount의 특권·post-hook·경로 처리,
  CVE-2026-78408은 nsenter `--join-cgroup`에 관한 보고다.
  [mount upstream advisory](https://github.com/util-linux/util-linux/security/advisories/GHSA-g8wm-75wr-g2vh),
  [nsenter upstream advisory](https://github.com/util-linux/util-linux/security/advisories/GHSA-55fx-f4gg-cfhj).
  정확한 ARM64 package inventory에는 `libuuid@2.42.1-r0`과 BusyBox가 있고 util-linux mount,
  libmount, nsenter package가 없다. libuuid API에는 보고된 CLI 실행 경로가 없다.
  예외 purl은 libuuid에만 한정하며 libmount로 넓히는 변경을 mutation test가 거절한다.

## deunhealth

검토 대상은 upstream `internal/health/{client,server,handler}.go`, `internal/config/health.go`,
`internal/docker/{docker,labeled,restart,version}.go`와 잠금된 `goservices v0.1.0`의
`httpserver/server.go`다. health 요청은 `http://127.0.0.1:<port>/`로 고정된다. health 서버는
loopback에서 GET `/`만 처리하며 redirect·form·파일·HTML을 처리하지 않는다.
Docker client는 고정 `tcp://docker-proxy:2375`의 HTTP API에 연결한다. Moby daemon을 실행하지 않는다.
`otelhttp v0.64.0` client transport는 header `Inject`만 호출하고 baggage `Extract`를 호출하지 않는다.

| CVE | 도달하지 않는 취약 조건 |
| --- | --- |
| 2025-68121, 2026-27145, 32280, 32281, 32283, 33818, 56862 | TLS handshake/session/x509/ASN.1. 두 client 및 health server에 TLS가 없다. |
| 2026-33814, 56853 | HTTP/2 transport 또는 h2c 서버 opt-in. 기본 `http.Server`/plain transport로 활성화하지 않는다. |
| 2026-25679, 39821, 56860 | 제어되지 않은 IPv6/IDNA host 또는 상대 redirect 해석. health URL은 숫자 loopback과 `/`로 고정되고 Docker client는 redirect를 거절한다. |
| 2026-42504 | 악의적인 MIME 응답 header의 오류 처리. 입력은 자체 health 서버 또는 고정 Docker API의 응답이며 MIME 파서에 외부 문서를 전달하지 않는다. |
| 2026-34040, 41567, 42306 | Moby daemon의 AuthZ/plugin/archive/copy 구현. client의 목록·event·restart 호출은 해당 daemon 구현을 실행하지 않는다. |
| 2026-29181 | baggage header extraction. client의 propagation은 Inject이며 서버 계측 또는 Extract를 설치하지 않는다. |
| 2025-61726 | query/form parsing. health handler와 이 Docker client가 해당 parser를 호출하지 않는다. |
| 2026-25681, 27136, 33811, 39820, 39822, 42499, 46600, 56858, 56859 | HTML, LookupCNAME, mail, os.Root, x/net DNS, template, XML 등 사용하지 않는 API. inventory 보고이며 해당 취약 함수를 호출하지 않는다. |
| 2026-39836 | Windows-only net 동작. 대상은 linux/arm64다. |

위 표에서 연도 생략 번호는 같은 셀의 2026년을 따른다.
TLS·외부 health·env file·추가 command/mount/port를 추가하면 실행 조건을 재검토해야 한다.
checker와 mutation test는 고정 environment와 시작 방식을 검사한다.

## socket-proxy

upstream `cmd/socket-proxy/main.go`는 `http.Server.Serve`와 Unix socket 전용 `DialContext`를
설치한다. reverse proxy의 origin은 `http://localhost`이고 Docker HTTP 응답을 전달하며
상대 redirect를 따라가지 않는다. `checksocketconnection.go`의 health client/server는
localhost HTTP의 고정 `/health`와 HEAD만 사용한다. TLS 또는 h2c를 켜지 않는다.
`ProxyContainerName` 기본값은 빈 문자열이고 Compose에 이를 켜는 flag/env가 없어,
별도 `internal/docker/client`의 동적 allowlist 요청 경로가 비활성이다.

| CVE | 도달하지 않는 취약 조건 |
| --- | --- |
| 2026-33818, 56862 | ASN.1/TLS: 고정 Unix HTTP transport와 loopback HTTP에 TLS가 없다. |
| 2026-39821, 56860 | IDNA 또는 상대 경로 해석: host가 고정이고 reverse proxy가 redirect를 따라가지 않는다. 동적 client 경로도 꺼져 있다. |
| 2026-56853 | 명시적으로 켜야 하는 h2c. `Server.Protocols`/UnencryptedHTTP2가 설정되지 않는다. |
| 2026-46600, 56858, 56859 | DNS parser, HTML template, XML decoder의 취약 함수는 호출하지 않는다. |

[Go의 h2c 조건](https://pkg.go.dev/vuln/GO-2026-6089)과
[상대 경로 해석 조건](https://pkg.go.dev/vuln/GO-2026-6218)을 정확한 source에 대조했다.
동적 allowlist·socket endpoint·추가 environment/entrypoint 변경은 checker가 거절한다.
신규 관리 웹은 이 proxy 네트워크나 Docker socket을 사용하지 않는다. 이 예외 재검토로 웹 권한을 추가하지 않는다.

## 제거와 재검토 조건

수정된 고정 upstream 이미지의 검증 후 해당 CVE tuple을 제거한다. 자동 기한 연장은 없으며
2026-09-21 UTC에는 재검토 없이는 gate가 다시 실패한다. 그 이전에도 새로운 CVE/package,
source/image digest, CLI 설정, health bind, TLS/h2c 또는 입력 출처 변경은 이 판단에 포함되지 않는다.
