#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/.." && pwd)

BIN_DIR="${HOME}/.local/bin"
CONFIG_DIR="${HOME}/.config/coe"
SYSTEMD_DIR="${HOME}/.config/systemd/user"
UNIT_PATH="${SYSTEMD_DIR}/coe.service"
ENV_PATH="${CONFIG_DIR}/env"
GNOME_EXTENSIONS_DIR="${HOME}/.local/share/gnome-shell/extensions"
GNOME_FOCUS_HELPER_UUID="coe-focus-helper@mistermorph.com"
GNOME_FOCUS_HELPER_SRC="${REPO_ROOT}/packaging/gnome-shell-extension/${GNOME_FOCUS_HELPER_UUID}"
GNOME_FOCUS_HELPER_DST="${GNOME_EXTENSIONS_DIR}/${GNOME_FOCUS_HELPER_UUID}"

mkdir -p "${BIN_DIR}" "${CONFIG_DIR}" "${SYSTEMD_DIR}" "${GNOME_EXTENSIONS_DIR}"

echo "building coe -> ${BIN_DIR}/coe"
(cd "${REPO_ROOT}" && go build -o "${BIN_DIR}/coe" ./cmd/coe)

echo "ensuring default config exists"
"${BIN_DIR}/coe" config init >/dev/null || true

if [[ ! -f "${ENV_PATH}" ]]; then
  cat >"${ENV_PATH}" <<'EOF'
OPENAI_API_KEY=
EOF
  chmod 600 "${ENV_PATH}"
  echo "wrote ${ENV_PATH}"
fi

install -m 0644 "${REPO_ROOT}/packaging/systemd/coe.service" "${UNIT_PATH}"

if [[ -d "${GNOME_FOCUS_HELPER_SRC}" ]]; then
  echo "installing GNOME focus helper -> ${GNOME_FOCUS_HELPER_DST}"
  rm -rf "${GNOME_FOCUS_HELPER_DST}"
  cp -r "${GNOME_FOCUS_HELPER_SRC}" "${GNOME_FOCUS_HELPER_DST}"

  if command -v gnome-extensions >/dev/null 2>&1; then
    gnome-extensions enable "${GNOME_FOCUS_HELPER_UUID}" || true
  fi
fi

systemctl --user import-environment \
  DISPLAY \
  WAYLAND_DISPLAY \
  XDG_CURRENT_DESKTOP \
  XDG_SESSION_TYPE \
  DBUS_SESSION_BUS_ADDRESS \
  XDG_RUNTIME_DIR || true

systemctl --user daemon-reload
systemctl --user enable --now coe.service

echo
echo "Coe user service installed."
echo
echo "Next steps:"
echo "1. Put your OpenAI key in ${ENV_PATH}"
echo "2. Restart the service: systemctl --user restart coe.service"
echo "3. Check logs: journalctl --user -u coe.service -f"
echo "4. Coe will try to ensure the GNOME fallback shortcut from hotkey.preferred_accelerator when the service starts"
echo "5. Focus-aware paste is enabled by default in new configs. If your config already exists, set output.use_gnome_focus_helper: true in ${CONFIG_DIR}/config.yaml"
