# shellcheck shell=bash
# Full rootfs/ownership verification and image export occur only on kapu.
# Runtime hosts inspect the loaded ID/revision/architecture against that reviewed artifact.
po_verify_image() {
  local image="$1" manifest="$2" revision="$3" arch="$4" id_file="$5"
  local actual_revision actual_arch actual_id expected_id
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$arch" == arm64 ]] || return 1
  expected_id="$(cat "$id_file")"
  [[ "$expected_id" =~ ^sha256:[0-9a-f]{64}$ ]] || return 1
  python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert m["schema_version"] == 1 and m["source_revision"] == sys.argv[2] and m["architecture"] == sys.argv[3]' "$manifest" "$revision" "$arch"
  actual_revision="$(sudo -n docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")"
  actual_arch="$(sudo -n docker image inspect -f '{{.Os}}/{{.Architecture}}' "$image")"
  actual_id="$(sudo -n docker image inspect -f '{{.Id}}' "$image")"
  [[ "$actual_revision" == "$revision" && "$actual_arch" == "linux/$arch" && "$actual_id" == "$expected_id" ]] || {
    echo 'loaded issuer image ID/revision/architecture differs from reviewed kapu artifact' >&2
    return 1
  }
}
