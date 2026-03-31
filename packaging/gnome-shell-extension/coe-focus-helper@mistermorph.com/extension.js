import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

const OBJECT_PATH = '/org/gnome/Shell/Extensions/FocusWmClass';
const COE_SERVICE_NAME = 'com.mistermorph.Coe';
const COE_OBJECT_PATH = '/com/mistermorph/Coe';
const FCITX_RUNTIME_MODE = 'fcitx';
const DESKTOP_RUNTIME_MODE = 'desktop';
const INTERFACE_XML = `
<node>
  <interface name="org.gnome.Shell.Extensions.FocusWmClass">
    <method name="Get">
      <arg name="wm_class" type="s" direction="out"/>
    </method>
  </interface>
</node>`;
const COE_INTERFACE_XML = `
<node>
  <interface name="com.mistermorph.Coe.Dictation1">
    <method name="RuntimeMode">
      <arg name="runtime_mode" type="s" direction="out"/>
    </method>
    <method name="CurrentScene">
      <arg name="scene_id" type="s" direction="out"/>
      <arg name="display_name" type="s" direction="out"/>
    </method>
    <method name="ListScenes">
      <arg name="scenes_json" type="s" direction="out"/>
    </method>
    <method name="SwitchScene">
      <arg name="scene_id" type="s" direction="in"/>
    </method>
    <signal name="SceneChanged">
      <arg name="scene_id" type="s"/>
      <arg name="display_name" type="s"/>
    </signal>
  </interface>
</node>`;

const CoeProxy = Gio.DBusProxy.makeProxyWrapper(COE_INTERFACE_XML);
const LOG_PREFIX = '[coe-shell-helper]';

function emitInfo(message) {
  log(`${LOG_PREFIX} ${message}`);
}

function emitError(message, error = null) {
  if (error)
    logError(error, `${LOG_PREFIX} ${message}`);
  else
    log(`${LOG_PREFIX} ${message}`);
}

class FocusService {
  constructor(extension) {
    this._extension = extension;
  }

  Get() {
    return this._extension.currentWmClass;
  }
}

export default class CoeFocusHelperExtension extends Extension {
  enable() {
    emitInfo('enable()');
    this.currentWmClass = '';
    this._signalId = global.display.connect('notify::focus-window', () => {
      this._refreshFocusedWindow();
    });
    this._refreshFocusedWindow();

    this._service = new FocusService(this);
    this._exportedObject = Gio.DBusExportedObject.wrapJSObject(INTERFACE_XML, this._service);
    this._exportedObject.export(Gio.DBus.session, OBJECT_PATH);
    emitInfo(`exported focus service on ${OBJECT_PATH}`);

    this._buildIndicator();
    this._connectCoeProxy();
  }

  disable() {
    emitInfo('disable()');
    if (this._signalId) {
      global.display.disconnect(this._signalId);
      this._signalId = null;
    }

    this._exportedObject?.unexport();
    this._exportedObject = null;
    this._service = null;
    if (this._menuSignalId) {
      this._indicator.menu.disconnect(this._menuSignalId);
      this._menuSignalId = 0;
    }
    if (this._coeProxy && this._proxySignalId) {
      this._coeProxy.disconnectSignal(this._proxySignalId);
      this._proxySignalId = 0;
    }
    this._coeProxy = null;
    if (this._indicator) {
      this._indicator.destroy();
      this._indicator = null;
    }
    this.currentWmClass = '';
  }

  _refreshFocusedWindow() {
    const window = global.display.focus_window;
    if (!window) {
      this.currentWmClass = '';
      return;
    }

    try {
      this.currentWmClass = window.get_wm_class?.() ?? '';
    } catch (_) {
      this.currentWmClass = '';
    }
  }

  _buildIndicator() {
    emitInfo('building panel indicator');
    this._indicator = new PanelMenu.Button(0.0, 'Coe');
    const indicatorIcon = Gio.icon_new_for_string(
      GLib.build_filenamev([this.path, 'coe-symbolic.svg']));
    this._indicator.add_child(new St.Icon({
      gicon: indicatorIcon,
      style_class: 'system-status-icon',
    }));

    this._scenesItem = new PopupMenu.PopupSubMenuMenuItem('Scenes');
    this._indicator.menu.addMenuItem(this._scenesItem);
    this._indicator.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

    this._restartItem = new PopupMenu.PopupMenuItem('Restart Coe');
    this._restartItem.connect('activate', () => {
      this._restartCoe();
    });
    this._indicator.menu.addMenuItem(this._restartItem);

    this._menuSignalId = this._indicator.menu.connect('open-state-changed', (_menu, isOpen) => {
      if (isOpen)
        this._refreshScenes();
    });

    Main.panel.addToStatusArea('coe-focus-helper', this._indicator);
    emitInfo('panel indicator added to status area');
    this._renderScenes([]);
  }

  _connectCoeProxy() {
    emitInfo('connecting to Coe D-Bus proxy');
    this._coeProxy = new CoeProxy(
      Gio.DBus.session,
      COE_SERVICE_NAME,
      COE_OBJECT_PATH,
      (_proxy, error) => {
        if (error) {
          emitError('failed to connect to Coe D-Bus proxy', error);
          this._renderScenes([]);
          return;
        }
        emitInfo('connected to Coe D-Bus proxy');
        this._proxySignalId = this._coeProxy.connectSignal('SceneChanged', () => {
          emitInfo('received SceneChanged signal');
          this._refreshScenes();
        });
        this._refreshScenes();
      });
  }

  _refreshScenes() {
    if (!this._coeProxy) {
      emitInfo('scene refresh skipped because proxy is unavailable');
      this._renderScenes([]);
      return;
    }

    emitInfo('requesting scene list');
    this._coeProxy.ListScenesRemote((result, error) => {
      if (error) {
        emitError('ListScenes failed', error);
        this._renderScenes([]);
        return;
      }

      try {
        const [scenesJson] = result;
        const scenes = JSON.parse(scenesJson);
        emitInfo(`scene list loaded: ${scenesJson}`);
        this._renderScenes(Array.isArray(scenes) ? scenes : []);
      } catch (e) {
        emitError('failed to parse scene list', e);
        this._renderScenes([]);
        Main.notifyError('Coe', `Failed to parse scene list: ${e.message}`);
      }
    });
  }

  _renderScenes(scenes) {
    emitInfo(`rendering scenes menu with ${scenes.length} item(s)`);
    this._scenesItem.menu.removeAll();

    if (!scenes.length) {
      const item = new PopupMenu.PopupMenuItem('Coe unavailable');
      item.setSensitive(false);
      this._scenesItem.menu.addMenuItem(item);
      return;
    }

    for (const scene of scenes) {
      this._scenesItem.menu.addMenuItem(this._buildSceneItem(scene));
    }
  }

  _buildSceneItem(scene) {
    const item = new PopupMenu.PopupBaseMenuItem();
    const markerSlot = new St.Bin({
      style: 'width: 24px;',
      x_expand: false,
      y_expand: false,
    });

    if (scene.current) {
      markerSlot.set_child(new St.Icon({
        icon_name: 'media-record-symbolic',
        style_class: 'popup-menu-icon',
      }));
    }

    const label = new St.Label({
      text: scene.display_name ?? scene.id ?? '',
      x_expand: true,
    });

    item.add_child(markerSlot);
    item.add_child(label);
    item.label_actor = label;
    item.connect('activate', () => {
      this._switchScene(scene.id);
    });
    return item;
  }

  _switchScene(sceneId) {
    if (!this._coeProxy || !sceneId)
      return;

    emitInfo(`switching scene to ${sceneId}`);
    this._coeProxy.SwitchSceneRemote(sceneId, (_result, error) => {
      if (error) {
        emitError(`failed to switch scene to ${sceneId}`, error);
        Main.notifyError('Coe', `Failed to switch scene: ${error.message}`);
        return;
      }
      emitInfo(`switched scene to ${sceneId}`);
      this._refreshScenes();
    });
  }

  _restartCoe() {
    this._detectRuntimeMode(runtimeMode => {
      const normalizedMode = this._normalizeRuntimeMode(runtimeMode);
      const shouldRestartFcitx = normalizedMode === FCITX_RUNTIME_MODE;
      emitInfo(`restarting coe.service (runtime.mode=${normalizedMode})`);
      this._runCommand(
        ['systemctl', '--user', 'restart', 'coe.service'],
        'coe.service restart',
        () => {
          emitInfo('coe.service restart completed');
          if (!shouldRestartFcitx) {
            this._scheduleScenesRefresh();
            return;
          }

          emitInfo('runtime.mode=fcitx, restarting fcitx5');
          this._runCommand(
            ['fcitx5', '-rd'],
            'fcitx5 restart',
            () => {
              emitInfo('fcitx5 restart completed');
              this._scheduleScenesRefresh();
            },
            error => {
              emitError('failed to restart fcitx5', error);
              Main.notifyError('Coe', `Restarted Coe, but failed to restart fcitx5: ${error.message}`);
              this._scheduleScenesRefresh();
            });
        },
        error => {
          emitError('failed to restart coe.service', error);
          Main.notifyError('Coe', `Failed to restart Coe: ${error.message}`);
        });
    });
  }

  _detectRuntimeMode(callback) {
    if (this._coeProxy?.RuntimeModeRemote) {
      this._coeProxy.RuntimeModeRemote((result, error) => {
        if (error) {
          emitError('RuntimeMode failed, falling back to config file', error);
          callback(this._readRuntimeModeFromConfig());
          return;
        }

        const [runtimeMode] = result ?? [];
        callback(this._normalizeRuntimeMode(runtimeMode));
      });
      return;
    }

    callback(this._readRuntimeModeFromConfig());
  }

  _readRuntimeModeFromConfig() {
    const configPath = GLib.getenv('COE_CONFIG') ||
      GLib.build_filenamev([GLib.get_home_dir(), '.config', 'coe', 'config.yaml']);

    try {
      const [ok, contents] = GLib.file_get_contents(configPath);
      if (!ok)
        return DESKTOP_RUNTIME_MODE;

      return this._parseRuntimeMode(new TextDecoder().decode(contents));
    } catch (error) {
      emitError(`failed to read runtime.mode from ${configPath}`, error);
      return DESKTOP_RUNTIME_MODE;
    }
  }

  _parseRuntimeMode(contents) {
    let inRuntime = false;

    for (const rawLine of contents.split('\n')) {
      const line = rawLine.replace(/\r$/, '');
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith('#'))
        continue;

      if (!line.startsWith(' ') && !line.startsWith('\t')) {
        inRuntime = /^runtime\s*:\s*$/.test(trimmed);
        continue;
      }

      if (!inRuntime)
        continue;

      const match = line.match(/^\s*mode\s*:\s*([^#\s]+|"[^"]+"|'[^']+')\s*(?:#.*)?$/);
      if (!match)
        continue;

      return this._normalizeRuntimeMode(match[1]);
    }

    return FCITX_RUNTIME_MODE;
  }

  _normalizeRuntimeMode(value) {
    const normalized = `${value ?? ''}`.trim().replace(/^['"]|['"]$/g, '').toLowerCase();
    if (normalized === FCITX_RUNTIME_MODE)
      return FCITX_RUNTIME_MODE;
    if (normalized === DESKTOP_RUNTIME_MODE)
      return DESKTOP_RUNTIME_MODE;
    return DESKTOP_RUNTIME_MODE;
  }

  _runCommand(argv, description, onSuccess, onFailure) {
    try {
      const proc = Gio.Subprocess.new(argv, Gio.SubprocessFlags.NONE);
      proc.wait_check_async(null, (subprocess, result) => {
        try {
          subprocess.wait_check_finish(result);
          onSuccess?.();
        } catch (error) {
          onFailure?.(error);
        }
      });
    } catch (error) {
      emitError(`failed to spawn ${description}`, error);
      onFailure?.(error);
    }
  }

  _scheduleScenesRefresh() {
    GLib.timeout_add(GLib.PRIORITY_DEFAULT, 1000, () => {
      this._refreshScenes();
      return GLib.SOURCE_REMOVE;
    });
  }
}
