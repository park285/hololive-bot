# shellcheck shell=bash
# rootfs·소유권 검증과 이미지 내보내기는 kapu에서만 수행한다.
# 런타임 호스트는 적재한 이미지의 ID·리비전·아키텍처를 검토된 산출물과 대조한다.
# 런타임은 호출자가 호스트로 정한다(중앙 hololive-seoul=podman, Seoul AP=docker). 자동 선택이나 다른 런타임으로의
# 재시도는 하지 않는다. identity는 sha256:<hex>이며 Podman .Id는 접두어가 없어 붙여서 비교한다.
po_image_id_matches() {
  local actual="$1" identities="$2" expected count=0 matched=false
  # Docker buildx 시절 영수증은 manifest·config digest 두 줄일 수 있어 이전 pair rollback을 위해 두 줄까지 받는다.
  [[ "$actual" =~ ^sha256:[0-9a-f]{64}$ && ${#identities} -le 143 ]] || return 1
  while IFS= read -r expected; do
    [[ "$expected" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
    count=$((count + 1))
    (( count <= 2 )) || return 1
    if [[ "$actual" == "$expected" ]]; then matched=true; fi
  done <<< "$identities"
  [[ "$matched" == true ]]
}

po_image_identity() {
  local runtime="$1" image="$2" id
  case "$runtime" in
    podman) id="$(sudo -n podman image inspect -f '{{.Id}}' "$image")" || return 1; id="sha256:$id" ;;
    docker) id="$(sudo -n docker image inspect -f '{{.Id}}' "$image")" || return 1 ;;
    *) echo "unsupported image runtime: $runtime" >&2; return 1 ;;
  esac
  printf '%s\n' "$id"
}

# kapu archive가 담는 identity는 config digest뿐이다. Docker는 classic overlay2 image store일 때만 docker load 뒤
# .Id로 같은 config digest를 보고하고, containerd image store(Driver=overlayfs 등)는 load 때 만든 manifest
# digest를 보고한다. 이 전제를 확인하지 못하면 대조가 성립하지 않으므로 적재·태그 변경 전에 실패한다.
po_require_docker_config_digest_store() {
  local driver
  driver="$(sudo -n docker info --format '{{.Driver}}')" || { echo 'cannot read AP Docker storage driver' >&2; return 1; }
  [[ "$driver" == overlay2 ]] || {
    echo "AP Docker image store must be classic overlay2 to verify kapu docker-archive identity (driver=$driver)" >&2
    return 1
  }
}

po_verify_image() {
  local runtime="$1" image="$2" manifest="$3" revision="$4" arch="$5" id_file="$6"
  local actual_revision actual_arch actual_id expected_id
  [[ "$runtime" == docker || "$runtime" == podman ]] || return 1
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$arch" == arm64 ]] || return 1
  expected_id="$(cat "$id_file")"
  python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert m["schema_version"] == 1 and m["source_revision"] == sys.argv[2] and m["architecture"] == sys.argv[3]' "$manifest" "$revision" "$arch"
  actual_revision="$(sudo -n "$runtime" image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
  actual_arch="$(sudo -n "$runtime" image inspect -f '{{.Os}}/{{.Architecture}}' "$image")"
  actual_id="$(po_image_identity "$runtime" "$image")"
  [[ "$actual_revision" == "$revision" && "$actual_arch" == "linux/$arch" ]] && po_image_id_matches "$actual_id" "$expected_id" || {
    echo "loaded issuer image ID/revision/architecture differs from reviewed kapu artifact (runtime=$runtime)" >&2
    return 1
  }
}
