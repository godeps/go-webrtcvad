#!/usr/bin/env bash
#set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${REPO_ROOT}"

DEFAULT_ZIG_HOME="${REPO_ROOT}/zig-x86_64-linux-0.15.2"
ZIG_HOME="${ZIG_HOME:-$DEFAULT_ZIG_HOME}"
echo "Using ZIG_HOME=${ZIG_HOME}" >&2
ZIG_BIN="${ZIG_HOME}/zig"

ensure_zig() {
	if [[ -x "${ZIG_BIN}" ]]; then
		return 0
	fi

	local url="https://ziglang.org/download/0.15.2/zig-x86_64-linux-0.15.2.tar.xz"
	local tarball="${REPO_ROOT}/zig-x86_64-linux-0.15.2.tar.xz"

	echo "zig 0.15.2 not found, downloading..."
	curl -L "${url}" -o "${tarball}"
	tar -xf "${tarball}" -C "${REPO_ROOT}"
	rm -f "${tarball}"
	echo "zig extracted, verifying ${ZIG_BIN}" >&2
	if [[ ! -x "${ZIG_BIN}" ]]; then
		ls -la "$(dirname "${ZIG_BIN}")" >&2 || true
		echo "error: failed to install zig at ${ZIG_BIN}" >&2
		exit 1
	fi
}

ensure_zig

BUILD_DIR="${REPO_ROOT}/wasm-bridge/build"
mkdir -p "${BUILD_DIR}"

INCLUDE_DIR="${REPO_ROOT}/webrtc_lkgr"
OUT_WASM="${BUILD_DIR}/webrtcvad_bridge.wasm"

SOURCES=(
  "${REPO_ROOT}/wasm-bridge/src/webrtcvad_bridge.c"
  "${INCLUDE_DIR}/common_audio/signal_processing/resample_by_2_internal.c"
  "${INCLUDE_DIR}/common_audio/signal_processing/spl.c"
  "${INCLUDE_DIR}/common_audio/vad/vad_filterbank.c"
  "${INCLUDE_DIR}/common_audio/vad/vad_core.c"
  "${INCLUDE_DIR}/common_audio/vad/vad_gmm.c"
  "${INCLUDE_DIR}/common_audio/vad/vad_sp.c"
  "${INCLUDE_DIR}/common_audio/vad/webrtc_vad.c"
)

echo "Building webrtcvad wasm bridge with ${ZIG_BIN}"
"${ZIG_BIN}" cc \
  -target wasm32-wasi \
  -O3 \
  -std=gnu89 \
  -mexec-model=reactor \
  -fwrapv \
  -I"${INCLUDE_DIR}" \
  "${SOURCES[@]}" \
  -Wl,--export=malloc \
  -Wl,--export=free \
  -Wl,--export=bridge_vad_create \
  -Wl,--export=bridge_vad_free \
  -Wl,--export=bridge_vad_init \
  -Wl,--export=bridge_vad_set_mode \
  -Wl,--export=bridge_vad_process \
  -Wl,--export=bridge_vad_valid_rate_and_frame_length \
  -Wl,--no-entry \
  -Wl,--export-memory \
  -o "${OUT_WASM}"

echo "Wasm bridge written to ${OUT_WASM}"
