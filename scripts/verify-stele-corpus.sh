#!/usr/bin/env bash
set -euo pipefail

stele_version="${STEELE_VERSION:-0.0.2}"
corpus_path="pkg/protocol/testdata/stele.causal-contract.v1.json"
workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

package_name="$(npm pack "@noopolis/stele@${stele_version}" --silent --pack-destination "${workdir}")"
tar -xOf "${workdir}/${package_name}" \
  package/src/contracts/goldens/causal-contract.v1.json \
  | cmp - "${corpus_path}"

printf 'verified %s against @noopolis/stele@%s\n' "${corpus_path}" "${stele_version}"
