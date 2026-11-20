// OpenPeats in GNOME's Quick Settings: a toggle for the earbuds' noise control,
// with the modes and the battery levels in its menu.
//
// The extension holds no link to the earbuds and speaks no earbud protocol. It
// is a view on the control service of the OpenPeats application, on the
// session bus, and every action is a call to it. When the application is not
// running, the first call starts it in the background through D-Bus
// activation: the toggle works from a cold session, as the Android tile does.

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import {QuickMenuToggle, SystemIndicator} from 'resource:///org/gnome/shell/ui/quickSettings.js';

const BUS_NAME = 'io.github.wasabules.OpenPeats';
const OBJECT_PATH = '/io/github/wasabules/OpenPeats';

// A copy of the interface in desktop/internal/freedesktop/service.go: a change
// there comes here too.
const INTERFACE = `<node>
  <interface name="io.github.wasabules.OpenPeats1">
    <method name="Activate"/>
    <method name="Connect"/>
    <method name="Disconnect"/>
    <method name="SetANCMode"><arg name="mode" type="i" direction="in"/></method>
    <method name="CycleANCMode"/>
    <property name="Known" type="b" access="read"/>
    <property name="Connected" type="b" access="read"/>
    <property name="Connecting" type="b" access="read"/>
    <property name="DeviceName" type="s" access="read"/>
    <property name="ANCMode" type="i" access="read"/>
    <property name="ANCSupported" type="b" access="read"/>
    <property name="BatteryLeft" type="i" access="read"/>
    <property name="BatteryRight" type="i" access="read"/>
    <property name="BatteryCase" type="i" access="read"/>
  </interface>
</node>`;

const OpenPeatsProxy = Gio.DBusProxy.makeProxyWrapper(INTERFACE);

const ICON = 'audio-headphones-symbolic';

// The modes in the order a one-button control goes through them.
const MODES = [
    {code: 0, key: 'mode.off'},
    {code: 1, key: 'mode.anc'},
    {code: 2, key: 'mode.transparency'},
];

/**
 * The extension's own strings, in locales/<lang>.json. English is the source
 * and the fallback for any key a translation misses, as everywhere in
 * OpenPeats; the language is the shell's.
 */
class Catalogue {
    constructor(dir) {
        const read = lang => {
            try {
                const file = dir.get_child('locales').get_child(`${lang}.json`);
                const [, bytes] = file.load_contents(null);
                return JSON.parse(new TextDecoder().decode(bytes));
            } catch {
                return null;
            }
        };
        this._source = read('en') ?? {};
        this._own = null;
        for (const name of GLib.get_language_names()) {
            const lang = name.split(/[_.@]/)[0];
            if (lang === 'C' || lang === 'en')
                break;
            this._own = read(lang);
            if (this._own)
                break;
        }
    }

    t(key, args = {}) {
        const text = this._own?.[key] ?? this._source[key] ?? key;
        return text.replace(/\{(\w+)\}/g, (match, name) =>
            name in args ? String(args[name]) : match);
    }
}

const EarbudsToggle = GObject.registerClass(
class EarbudsToggle extends QuickMenuToggle {
    _init(catalogue) {
        const t = catalogue.t.bind(catalogue);
        super._init({
            title: t('title'),
            iconName: ICON,
            toggleMode: false,
            menuButtonAccessibleName: t('menuButton'),
        });
        this._t = t;

        this.menu.setHeader(ICON, t('title'));

        this._modeItems = new Map();
        for (const mode of MODES) {
            const item = new PopupMenu.PopupMenuItem(t(mode.key));
            item.connect('activate', () => this._call('SetANCMode', mode.code));
            this.menu.addMenuItem(item);
            this._modeItems.set(mode.code, item);
        }

        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this._linkItem = this.menu.addAction('', () => {
            this._call(this._proxy?.Connected ? 'Disconnect' : 'Connect');
        });
        this.menu.addAction(t('open'), () => {
            Main.panel.closeQuickSettings();
            this._call('Activate');
        });

        this.connect('clicked', () => this._onClicked());

        this._proxy = null;
        this._proxySignals = [];
        this._cancellable = new Gio.Cancellable();
        // Not started at construction: the extension loads with the shell,
        // and the application should not start with it. A method call may
        // start it, which is what a click asks for.
        new OpenPeatsProxy(Gio.DBus.session, BUS_NAME, OBJECT_PATH, (proxy, error) => {
            if (error) {
                if (!error.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED))
                    console.error(`OpenPeats: no proxy for ${BUS_NAME}: ${error.message}`);
                return;
            }
            this._proxy = proxy;
            this._proxySignals = [
                proxy.connect('g-properties-changed', () => this._sync()),
                proxy.connect('notify::g-name-owner', () => this._sync()),
            ];
            this._sync();
        }, this._cancellable, Gio.DBusProxyFlags.DO_NOT_AUTO_START_AT_CONSTRUCTION);

        this.connect('destroy', () => {
            this._cancellable.cancel();
            for (const id of this._proxySignals)
                this._proxy.disconnect(id);
            this._proxySignals = [];
            this._proxy = null;
        });

        this._sync();
    }

    // The state the application publishes, or null when it is not running:
    // the proxy drops its cached properties when the name loses its owner.
    _state() {
        const p = this._proxy;
        if (!p?.g_name_owner)
            return null;
        return {
            known: p.Known,
            connected: p.Connected,
            connecting: p.Connecting,
            name: p.DeviceName,
            mode: p.ANCMode,
            ancSupported: p.ANCSupported,
            left: p.BatteryLeft,
            right: p.BatteryRight,
            caseLevel: p.BatteryCase,
        };
    }

    _onClicked() {
        const s = this._state();
        if (s?.connected) {
            this._call('CycleANCMode');
        } else if (s && !s.known) {
            // No earbuds picked yet: that happens in the window.
            Main.panel.closeQuickSettings();
            this._call('Activate');
        } else {
            this._call('Connect');
        }
    }

    _call(method, ...args) {
        if (!this._proxy)
            return;
        this._proxy[`${method}Async`](...args).catch(error => {
            // No OpenPeats installed, or its D-Bus service file missing:
            // nothing could start it.
            console.error(`OpenPeats: ${method} failed: ${error.message}`);
        });
    }

    _batteries(s) {
        const t = this._t;
        return [
            ['battery.left', s.left],
            ['battery.right', s.right],
            ['battery.case', s.caseLevel],
        ]
            .filter(([, level]) => level >= 0)
            .map(([key, level]) => t(key, {percent: t('percent', {value: level})}))
            .join(' · ');
    }

    _sync() {
        const t = this._t;
        const s = this._state();
        const mode = MODES.find(m => m.code === s?.mode);

        let subtitle;
        if (s && !s.known)
            subtitle = t('noEarbuds');
        else if (s?.connecting)
            subtitle = t('connecting');
        else if (!s?.connected)
            subtitle = t('disconnected');
        else
            subtitle = mode ? t(mode.key) : '';

        const title = (s?.connected && s.name) || t('title');
        this.set({
            title,
            subtitle,
            checked: !!s?.connected && s.mode > 0,
        });
        this.menu.setHeader(ICON, title, s?.connected ? this._batteries(s) : subtitle);

        // Picking a mode while disconnected connects first; with no earbuds
        // picked, or earbuds without noise control, there is nothing to pick.
        const modesUsable = !s || (s.known && s.ancSupported);
        for (const [code, item] of this._modeItems) {
            item.setSensitive(modesUsable);
            item.setOrnament(s?.connected && s.mode === code
                ? PopupMenu.Ornament.CHECK : PopupMenu.Ornament.NONE);
        }

        this._linkItem.label.text = s?.connected ? t('disconnect') : t('connect');
        this._linkItem.visible = !s || s.known;
        this._linkItem.setSensitive(!s?.connecting);
    }
});

const Indicator = GObject.registerClass(
class Indicator extends SystemIndicator {
    _init(catalogue) {
        super._init();
        // No icon in the panel: the application puts its own there.
        this.quickSettingsItems.push(new EarbudsToggle(catalogue));
    }
});

export default class OpenPeatsExtension extends Extension {
    enable() {
        this._indicator = new Indicator(new Catalogue(this.dir));
        Main.panel.statusArea.quickSettings.addExternalIndicator(this._indicator);
    }

    disable() {
        this._indicator.quickSettingsItems.forEach(item => item.destroy());
        this._indicator.destroy();
        this._indicator = null;
    }
}
