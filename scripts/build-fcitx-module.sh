#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOURCE_DIR="${ROOT_DIR}/packaging/fcitx5"
BUILD_DIR="${BUILD_DIR:-/tmp/coe-fcitx5-build}"
INSTALL_SCOPE="${INSTALL_SCOPE:-user}"
EXTRA_CMAKE_ARGS=()

require_cmd() {
  if command -v "$1" >/dev/null 2>&1; then
    return 0
  fi
  echo "missing required command: $1" >&2
  exit 1
}

require_pkg() {
  if pkg-config --exists "$1"; then
    return 0
  fi
  echo "missing required pkg-config package: $1" >&2
  exit 1
}

require_cmd cmake
require_cmd pkg-config
require_cmd c++

require_pkg Fcitx5Core
require_pkg dbus-1

while (($# > 0)); do
  case "$1" in
    --system)
      INSTALL_SCOPE="system"
      shift
      ;;
    --user)
      INSTALL_SCOPE="user"
      shift
      ;;
    *)
      EXTRA_CMAKE_ARGS+=("$1")
      shift
      ;;
  esac
done

case "${INSTALL_SCOPE}" in
  user)
    INSTALL_PREFIX="${INSTALL_PREFIX:-${HOME}/.local}"
    FCITX_SYS_PATHS="OFF"
    LIBRARY_PATH="${INSTALL_PREFIX}/lib/x86_64-linux-gnu/fcitx5/libcoefcitx.so"
    ADDON_CONFIG_PATH="${INSTALL_PREFIX}/share/fcitx5/addon/coe.conf"
    ;;
  system)
    INSTALL_PREFIX="${INSTALL_PREFIX:-/usr}"
    FCITX_SYS_PATHS="ON"
    LIBRARY_PATH="/usr/lib/x86_64-linux-gnu/fcitx5/libcoefcitx.so"
    ADDON_CONFIG_PATH="/usr/share/fcitx5/addon/coe.conf"
    ;;
  *)
    echo "unsupported INSTALL_SCOPE: ${INSTALL_SCOPE}" >&2
    exit 1
    ;;
esac

cmake -S "${SOURCE_DIR}" -B "${BUILD_DIR}" \
  -DCMAKE_INSTALL_PREFIX="${INSTALL_PREFIX}" \
  -DFCITX_INSTALL_USE_FCITX_SYS_PATHS="${FCITX_SYS_PATHS}" \
  "${EXTRA_CMAKE_ARGS[@]}"

cmake --build "${BUILD_DIR}"

echo
echo "Fcitx module build completed."
echo "- install scope: ${INSTALL_SCOPE}"
echo "- source: ${SOURCE_DIR}"
echo "- build: ${BUILD_DIR}"
echo "- install prefix: ${INSTALL_PREFIX}"
echo "- artifact: ${BUILD_DIR}/libcoefcitx.so"
echo "- target library path: ${LIBRARY_PATH}"
echo "- target addon config path: ${ADDON_CONFIG_PATH}"
