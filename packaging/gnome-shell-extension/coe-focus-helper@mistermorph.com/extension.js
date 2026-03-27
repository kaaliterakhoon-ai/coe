import Gio from 'gi://Gio';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const OBJECT_PATH = '/org/gnome/Shell/Extensions/FocusWmClass';
const INTERFACE_XML = `
<node>
  <interface name="org.gnome.Shell.Extensions.FocusWmClass">
    <method name="Get">
      <arg name="wm_class" type="s" direction="out"/>
    </method>
  </interface>
</node>`;

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
    this._signalId = global.display.connect('notify::focus-window', () => {
      this._refreshFocusedWindow();
    });
    this._refreshFocusedWindow();

    this._service = new FocusService(this);
    this._exportedObject = Gio.DBusExportedObject.wrapJSObject(INTERFACE_XML, this._service);
    this._exportedObject.export(Gio.DBus.session, OBJECT_PATH);
  }

  disable() {
    if (this._signalId) {
      global.display.disconnect(this._signalId);
      this._signalId = null;
    }

    this._exportedObject?.unexport();
    this._exportedObject = null;
    this._service = null;
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
}
