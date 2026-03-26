import Gio from 'gi://Gio';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const BUS_NAME = 'org.quaily.Coe.Focus1';
const OBJECT_PATH = '/org/quaily/Coe/Focus1';
const INTERFACE_XML = `
<node>
  <interface name="org.quaily.Coe.Focus1">
    <method name="GetFocusedWindow">
      <arg name="app_id" type="s" direction="out"/>
      <arg name="wm_class" type="s" direction="out"/>
      <arg name="title" type="s" direction="out"/>
    </method>
  </interface>
</node>`;

class FocusService {
  GetFocusedWindow() {
    const window = global.display.get_focus_window();
    if (!window) {
      return ['', '', ''];
    }

    let appId = '';
    try {
      appId = window.get_sandboxed_app_id?.() ?? '';
    } catch (_) {
      appId = '';
    }
    if (!appId) {
      try {
        appId = window.get_gtk_application_id?.() ?? '';
      } catch (_) {
        appId = '';
      }
    }

    let wmClass = '';
    try {
      wmClass = window.get_wm_class?.() ?? '';
    } catch (_) {
      wmClass = '';
    }

    let title = '';
    try {
      title = window.get_title?.() ?? '';
    } catch (_) {
      title = '';
    }

    return [appId, wmClass, title];
  }
}

export default class CoeFocusHelperExtension extends Extension {
  enable() {
    this._ownerId = Gio.bus_own_name(
      Gio.BusType.SESSION,
      BUS_NAME,
      Gio.BusNameOwnerFlags.NONE,
      this._onBusAcquired.bind(this),
      null,
      null
    );
  }

  disable() {
    this._exportedObject?.unexport();
    this._exportedObject = null;
    this._service = null;

    if (this._ownerId) {
      Gio.bus_unown_name(this._ownerId);
      this._ownerId = 0;
    }
  }

  _onBusAcquired(connection) {
    this._service = new FocusService();
    this._exportedObject = Gio.DBusExportedObject.wrapJSObject(INTERFACE_XML, this._service);
    this._exportedObject.export(connection, OBJECT_PATH);
  }
}
