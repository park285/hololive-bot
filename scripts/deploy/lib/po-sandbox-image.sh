# shellcheck shell=bash
po_image_id_matches() {
  local actual="$1" expected="$2"
  [[ "$actual" =~ ^sha256:[0-9a-f]{64}$ && "$actual" == "$expected" ]]
}

po_verify_image() {
  local image="$1" manifest="$2" revision="$3" arch="$4" id_file="$5"
  local metadata actual_revision actual_arch actual_id expected_id
  [[ "$revision" =~ ^[0-9a-f]{40}$ && "$arch" == arm64 ]] || return 1
  expected_id="$(<"$id_file")"
  python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); assert m["schema_version"] == 1 and m["source_revision"] == sys.argv[2] and m["architecture"] == sys.argv[3]' "$manifest" "$revision" "$arch"
  metadata="$(sudo -n podman image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}} {{.Os}}/{{.Architecture}} {{.Id}}' "$image")"
  read -r actual_revision actual_arch actual_id <<<"$metadata"
  if ! { [[ "$actual_revision" == "$revision" && "$actual_arch" == "linux/$arch" ]] && po_image_id_matches "sha256:$actual_id" "$expected_id"; }; then
    echo "loaded issuer image ID/revision/architecture differs from reviewed kapu artifact" >&2
    return 1
  fi
}
