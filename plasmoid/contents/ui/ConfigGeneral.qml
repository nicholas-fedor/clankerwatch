// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kcmutils as KCM
import org.kde.kirigami as Kirigami
import org.kde.plasma.workspace.dbus as DBus

// Settings page. The daemon owns the settings file, so this page reads the
// daemon's Settings property and sends changes with SetSettings. Plasma's
// configuration dialog enables Apply from unsavedChanges and calls
// saveConfig() on Apply or OK.
KCM.SimpleKCM {
    id: page

    readonly property string busName: "com.nickfedor.ClankerWatch1"
    readonly property string objectPath: "/com/nickfedor/ClankerWatch1"

    // The daemon's Settings property, or null before it arrives.
    property var settings: null
    property string lastJson: ""
    property string errorText: ""
    property bool saving: false

    readonly property bool running: watcher.registered
    readonly property bool editable: running && settings !== null && settings.editable
    readonly property var patch: buildPatch()
    readonly property string thresholdError: notifyEnabled.checked ? validateThresholds(thresholdsField.text) : ""
    readonly property bool unsavedChanges: editable && Object.keys(patch).length > 0

    // Keys the page edits, for the locked-settings message.
    readonly property var labels: ({
        mode: i18n("Data source"),
        interval: i18n("Active interval"),
        idleInterval: i18n("Idle interval"),
        notify: i18n("Usage alerts"),
        notifyAuth: i18n("Login alert"),
    })

    readonly property string lockedText: {
        if (!settings) {
            return "";
        }
        const held = [];
        for (const key in labels) {
            if (settings.locked[key]) {
                held.push(i18nc("setting (source)", "%1 (%2)", labels[key], settings.locked[key]));
            }
        }
        return held.join(", ");
    }

    function locked(key: string): bool {
        return settings !== null && settings.locked[key] !== undefined;
    }

    function minutes(seconds: real): int {
        return Math.max(1, Math.round(seconds / 60));
    }

    // Parses "80, 95" into numbers. Returns null for invalid input.
    function parseThresholds(text: string): var {
        const out = [];
        for (const part of text.split(",")) {
            const field = part.trim();
            if (field === "") {
                continue;
            }
            const value = Number(field);
            if (!isFinite(value) || value <= 0 || value > 100) {
                return null;
            }
            out.push(value);
        }
        return out.sort((a, b) => a - b).filter((v, i, all) => i === 0 || v !== all[i - 1]);
    }

    function validateThresholds(text: string): string {
        const parsed = parseThresholds(text);
        if (parsed === null) {
            return i18n("Enter percentages between 1 and 100, separated by commas.");
        }
        return parsed.length === 0 ? i18n("Enter at least one percentage, or turn usage alerts off.") : "";
    }

    function sameList(a: var, b: var): bool {
        return a.length === b.length && a.every((v, i) => v === b[i]);
    }

    // The changes the controls hold, keyed like the SetSettings argument.
    function buildPatch(): var {
        const out = {};
        if (!settings) {
            return out;
        }
        if (modeBox.currentValue !== undefined && modeBox.currentValue !== settings.mode) {
            out.mode = modeBox.currentValue;
        }
        if (intervalBox.value !== minutes(settings.intervalSeconds)) {
            out.intervalSeconds = intervalBox.value * 60;
        }
        if (idleBox.value !== minutes(settings.idleIntervalSeconds)) {
            out.idleIntervalSeconds = idleBox.value * 60;
        }
        const thresholds = notifyEnabled.checked ? parseThresholds(thresholdsField.text) : [];
        if (thresholds !== null && !sameList(thresholds, settings.notify)) {
            out.notify = thresholds;
        }
        if (notifyAuthBox.checked !== settings.notifyAuth) {
            out.notifyAuth = notifyAuthBox.checked;
        }
        return out;
    }

    // Puts the daemon's values into the controls.
    function loadControls() {
        modeBox.currentIndex = Math.max(0, modeBox.indexOfValue(settings.mode));
        intervalBox.value = minutes(settings.intervalSeconds);
        idleBox.value = minutes(settings.idleIntervalSeconds);
        notifyEnabled.checked = settings.notify.length > 0;
        thresholdsField.text = settings.notify.length > 0 ? settings.notify.join(", ") : "80, 95";
        notifyAuthBox.checked = settings.notifyAuth;
    }

    function readSettings() {
        const raw = props.properties.Settings;
        // GetAll delivers a wrapped string and PropertiesChanged a plain one.
        const json = raw === undefined || raw === null ? "" : String(raw);
        if (json === "" || json === lastJson) {
            return;
        }
        try {
            const s = JSON.parse(json);
            if (!s || s.v !== 1) {
                return;
            }
            lastJson = json;
            const reload = settings === null || !unsavedChanges || saving;
            settings = s;
            if (reload) {
                loadControls();
            }
        } catch (e) {
            console.warn("clankerwatch: unreadable settings", e);
        }
    }

    function saveConfig() {
        if (!unsavedChanges) {
            return;
        }
        if (thresholdError !== "") {
            errorText = thresholdError;
            return;
        }
        errorText = "";
        saving = true;
        DBus.SessionBus.asyncCall({
            service: busName,
            path: objectPath,
            iface: busName,
            member: "SetSettings",
            // A plain JS string marshals as "s". Passing a signature field
            // makes the plugin send the wrong argument types.
            arguments: [JSON.stringify(patch)],
        }, reply => {
            saving = false;
            const message = reply.value === undefined || reply.value === null ? "" : String(reply.value);
            if (message !== "") {
                errorText = message;
            }
        }, reply => {
            saving = false;
            errorText = reply.error.message;
        });
    }

    DBus.Properties {
        id: props
        busType: DBus.BusType.Session
        service: page.busName
        path: page.objectPath
        iface: page.busName
        onPropertiesChanged: page.readSettings()
        onRefreshed: page.readSettings()
    }

    DBus.DBusServiceWatcher {
        id: watcher
        busType: DBus.BusType.Session
        watchedService: page.busName
        onRegisteredChanged: {
            if (registered) {
                props.updateAll();
            }
        }
    }

    header: ColumnLayout {
        spacing: 0

        Kirigami.InlineMessage {
            Layout.fillWidth: true
            position: Kirigami.InlineMessage.Position.Header
            type: Kirigami.MessageType.Warning
            visible: !page.running || page.settings === null
            text: i18n("clankerwatch isn't running. The settings appear once the service starts.")
        }
        Kirigami.InlineMessage {
            Layout.fillWidth: true
            position: Kirigami.InlineMessage.Position.Header
            type: Kirigami.MessageType.Information
            visible: page.running && page.settings !== null && !page.settings.editable
            text: i18n("The service is running in demo mode, so settings can't be changed.")
        }
        Kirigami.InlineMessage {
            Layout.fillWidth: true
            position: Kirigami.InlineMessage.Position.Header
            type: Kirigami.MessageType.Information
            visible: page.editable && page.lockedText !== ""
            text: i18n("Set by an environment variable or command-line flag: %1.", page.lockedText)
        }
        Kirigami.InlineMessage {
            Layout.fillWidth: true
            position: Kirigami.InlineMessage.Position.Header
            type: Kirigami.MessageType.Error
            visible: page.errorText !== ""
            text: page.errorText
            showCloseButton: true
            onVisibleChanged: {
                if (!visible) {
                    page.errorText = "";
                }
            }
        }
    }

    Kirigami.FormLayout {
        enabled: page.editable

        QQC2.ComboBox {
            id: modeBox
            Kirigami.FormData.label: i18n("Data source:")
            enabled: !page.locked("mode")
            textRole: "text"
            valueRole: "value"
            model: [
                { text: i18n("Usage endpoint and Claude Code's cache"), value: "hybrid" },
                { text: i18n("Claude Code's cache only"), value: "cache-only" },
            ]
        }
        QQC2.Label {
            Layout.fillWidth: true
            Layout.maximumWidth: Kirigami.Units.gridUnit * 22
            wrapMode: Text.Wrap
            font: Kirigami.Theme.smallFont
            color: Kirigami.Theme.disabledTextColor
            text: modeBox.currentValue === "cache-only"
                ? i18n("Never reads Claude Code's login or uses the network. Usage updates only when Claude Code saves it.")
                : i18n("Reads Claude Code's login without changing it and asks the usage endpoint, using Claude Code's own result when it is newer.")
        }

        Item {
            Kirigami.FormData.isSection: true
        }

        RowLayout {
            Kirigami.FormData.label: i18n("Check usage every:")
            enabled: !page.locked("interval")

            QQC2.SpinBox {
                id: intervalBox
                from: page.settings ? page.minutes(page.settings.minIntervalSeconds) : 2
                to: 120
            }
            QQC2.Label {
                text: i18np("minute while Claude Code is in use", "minutes while Claude Code is in use", intervalBox.value)
            }
        }
        RowLayout {
            enabled: !page.locked("idleInterval")

            QQC2.SpinBox {
                id: idleBox
                from: intervalBox.value
                to: 1440
            }
            QQC2.Label {
                text: i18np("minute while it is idle", "minutes while it is idle", idleBox.value)
            }
        }

        Item {
            Kirigami.FormData.isSection: true
        }

        RowLayout {
            Kirigami.FormData.label: i18n("Notifications:")
            enabled: !page.locked("notify")

            QQC2.CheckBox {
                id: notifyEnabled
                text: i18n("When usage reaches")
            }
            QQC2.TextField {
                id: thresholdsField
                enabled: notifyEnabled.checked
                placeholderText: "80, 95"
                Layout.preferredWidth: Kirigami.Units.gridUnit * 6
            }
            QQC2.Label {
                text: i18nc("percent sign after the thresholds", "%")
            }
        }
        QQC2.Label {
            visible: page.thresholdError !== ""
            text: page.thresholdError
            color: Kirigami.Theme.negativeTextColor
            font: Kirigami.Theme.smallFont
        }
        QQC2.CheckBox {
            id: notifyAuthBox
            enabled: !page.locked("notifyAuth")
            text: i18n("When the Claude Code login needs attention")
        }

        Item {
            Kirigami.FormData.isSection: true
        }

        QQC2.Label {
            Kirigami.FormData.label: i18n("Settings file:")
            Layout.fillWidth: true
            Layout.maximumWidth: Kirigami.Units.gridUnit * 22
            visible: page.settings !== null
            text: page.settings ? page.settings.file : ""
            textFormat: Text.PlainText
            wrapMode: Text.WrapAnywhere
            font: Kirigami.Theme.smallFont
            color: Kirigami.Theme.disabledTextColor
        }
    }
}
