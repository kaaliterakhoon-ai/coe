#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-/tmp/coe-fcitx5-build}"
SKIP_FCITX_RESTART="${SKIP_FCITX_RESTART:-0}"
INSTALL_SCOPE="${INSTALL_SCOPE:-user}"
SCRIPT_ARGS=()

while (($# > 0)); do
  case "$1" in
    --system)
      INSTALL_SCOPE="system"
      SCRIPT_ARGS+=("$1")
      shift
      ;;
    --user)
      INSTALL_SCOPE="user"
      SCRIPT_ARGS+=("$1")
      shift
      ;;
    *)
      SCRIPT_ARGS+=("$1")
      shift
      ;;
  esac
done

case "${INSTALL_SCOPE}" in
  user)
    INSTALL_PREFIX="${INSTALL_PREFIX:-${HOME}/.local}"
    LIBRARY_PATH="${INSTALL_PREFIX}/lib/x86_64-linux-gnu/fcitx5/libcoefcitx.so"
    ADDON_CONFIG_PATH="${INSTALL_PREFIX}/share/fcitx5/addon/coe.conf"
    ;;
  system)
    INSTALL_PREFIX="${INSTALL_PREFIX:-/usr}"
    LIBRARY_PATH="/usr/lib/x86_64-linux-gnu/fcitx5/libcoefcitx.so"
    ADDON_CONFIG_PATH="/usr/share/fcitx5/addon/coe.conf"
    if [[ "${EUID}" -ne 0 ]]; then
      echo "system install requires root. Re-run with sudo:" >&2
      echo "  sudo INSTALL_SCOPE=system ./scripts/install-fcitx-module.sh --system" >&2
      exit 1
    fi
    ;;
  *)
    echo "unsupported INSTALL_SCOPE: ${INSTALL_SCOPE}" >&2
    exit 1
    ;;
esac

restart_fcitx5() {
  if [[ "${INSTALL_SCOPE}" == "system" ]]; then
    echo "Skipping Fcitx5 restart in system install mode. Restart Fcitx5 from the target user session."
    return 0
  fi

  if [[ "${SKIP_FCITX_RESTART}" == "1" ]]; then
    echo "Skipping Fcitx5 restart because SKIP_FCITX_RESTART=1."
    return 0
  fi

  if ! command -v fcitx5 >/dev/null 2>&1; then
    echo "Fcitx5 binary not found. Log out and back in after install." >&2
    return 0
  fi

  if ! pgrep -x fcitx5 >/dev/null 2>&1; then
    echo "Fcitx5 is not running. The module will be picked up next time Fcitx5 starts."
    return 0
  fi

  echo "Restarting Fcitx5 with 'fcitx5 -rd'..."
  local restart_log
  restart_log="$(mktemp /tmp/coe-fcitx-restart.XXXXXX.log)"
  if fcitx5 -rd >"${restart_log}" 2>&1; then
    echo "Fcitx5 restart requested successfully."
    rm -f "${restart_log}"
    return 0
  fi

  echo "Failed to restart Fcitx5 automatically." >&2
  if [[ -s "${restart_log}" ]]; then
    echo "fcitx5 -rd output:" >&2
    sed -n '1,80p' "${restart_log}" >&2
  fi
  echo "Log out and back in, or run 'fcitx5 -rd' manually." >&2
  rm -f "${restart_log}"
  return 0
}

"${ROOT_DIR}/scripts/build-fcitx-module.sh" "${SCRIPT_ARGS[@]}"
cmake --install "${BUILD_DIR}"
restart_fcitx5

echo
echo "Fcitx module installed."
echo "- install scope: ${INSTALL_SCOPE}"
echo "- library: ${LIBRARY_PATH}"
echo "- addon config: ${ADDON_CONFIG_PATH}"
echo
echo "Next steps:"
if [[ "${INSTALL_SCOPE}" == "system" ]]; then
  echo "1. Restart Fcitx5 from the target user session, or log out and back in"
  echo "2. Make sure 'coe serve' is running with runtime.mode=fcitx"
  echo "3. Trigger the module with <Shift><Super>d inside an active Fcitx input context"
else
  echo "1. If the module does not show up immediately, try the system install path instead"
  echo "2. Make sure 'coe serve' is running with runtime.mode=fcitx"
  echo "3. Trigger the module with <Shift><Super>d inside an active Fcitx input context"
fi
