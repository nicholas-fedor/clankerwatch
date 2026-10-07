// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

pragma ComponentBehavior: Bound

import QtQuick
import org.kde.plasma.workspace.dbus as DBus

// Owns the D-Bus connection to the clankerwatch daemon and turns its snapshot
// into display-ready values. Every view binds to this object only.
QtObject {
    id: model

    readonly property string busName: "com.nickfedor.ClankerWatch1"
    readonly property string objectPath: "/com/nickfedor/ClankerWatch1"

    property var snapshot: null
    property string lastJson: ""
    // Wall clock for countdowns. It ticks once a minute and on demand.
    property double now: Date.now()

    readonly property bool serviceRunning: watcher.registered
    readonly property string status: !serviceRunning ? "not_running" : (snapshot ? snapshot.status : "starting")
    readonly property bool hasData: snapshot !== null && snapshot.fetchedAtMs > 0
    readonly property double ageMs: hasData ? Math.max(0, now - snapshot.fetchedAtMs) : 0
    readonly property bool isStale: !serviceRunning || !hasData || status === "token_expired" || ageMs > 3600000
    readonly property bool dimBars: status === "token_expired" || (hasData && ageMs > 3600000)
    readonly property string plan: snapshot ? snapshot.plan : ""

    readonly property var sessionBar: findBar("session")
    readonly property var weeklyBar: findBar("weekly_all")

    // Rows for the popup, in the order the daemon sends them.
    readonly property var rows: {
        const out = [];
        if (!snapshot) {
            return out;
        }
        for (const b of snapshot.bars) {
            const reset = b.resetsAtMs > 0 && b.resetsAtMs <= now;
            out.push({
                label: b.label,
                percent: reset ? 0 : b.percent,
                percentText: percentText(reset ? 0 : b.percent),
                level: reset ? 0 : b.level,
                leftText: reset ? i18n("Reset · awaiting next update")
                    : (b.resetsAtMs > 0 ? i18n("Resets in %1", duration(b.resetsAtMs - now)) : ""),
                rightText: reset || b.resetsAtMs <= 0 ? "" : clock(b.resetsAtMs),
            });
        }
        const x = snapshot.extraUsage;
        if (x) {
            out.push({
                label: i18n("Extra usage"),
                percent: x.percent === null ? 0 : x.percent,
                percentText: x.used,
                level: x.level,
                leftText: x.limitReached ? i18n("Monthly limit reached")
                    : (x.limit !== "" ? i18n("of %1 monthly limit", x.limit) : i18n("No monthly limit")),
                rightText: x.percent === null ? "" : i18n("%1 used", percentText(x.percent)),
            });
        }
        return out;
    }

    // The two bars the panel shows.
    readonly property var panelBars: [panelEntry(sessionBar, "5h"), panelEntry(weeklyBar, "7d")]

    // Replaces the bar list when there is nothing trustworthy to show.
    readonly property var emptyState: {
        switch (status) {
        case "not_running":
            return { title: i18n("clankerwatch isn't running"),
                     body: i18n("The background service should start with this widget. Check it with: systemctl --user status clankerwatch"),
                     button: i18n("Try again") };
        case "logged_out":
            return { title: i18n("Claude Code isn't signed in"),
                     body: i18n("Run claude in a terminal and sign in with /login. Usage appears here automatically."),
                     button: "" };
        case "auth_error":
            return { title: i18n("Claude Code's login was not accepted"),
                     body: snapshot.message + ". " + i18n("Run claude and sign in again with /login."),
                     button: "" };
        case "no_data":
            return { title: i18n("No usage data yet"),
                     body: i18n("Cache-only mode shows what Claude Code saved. Open /usage in Claude Code once."),
                     button: "" };
        }
        if (!hasData) {
            return { title: i18n("Loading usage…"), body: "", button: "" };
        }
        return null;
    }

    readonly property string footerText: {
        switch (status) {
        case "not_running": return i18n("Service unavailable");
        case "logged_out": return i18n("No credentials found");
        case "auth_error": return i18n("Login rejected");
        case "no_data": return i18n("Waiting for Claude Code");
        case "starting": return i18n("Starting");
        case "token_expired": return i18n("Paused until Claude Code is used again");
        case "rate_limited": return i18n("Rate limited · data from %1", ago(ageMs));
        case "offline": return i18n("Offline · data from %1", ago(ageMs));
        case "error": return i18n("Usage endpoint error · data from %1", ago(ageMs));
        }
        return ageMs < 60000 ? i18n("Updated just now") : i18n("Updated %1", ago(ageMs));
    }

    readonly property string footerRight: {
        if (!snapshot || !serviceRunning) {
            return "";
        }
        // The whole login lapses after a while and then needs /login.
        const loginLeft = snapshot.loginExpiresAtMs - now;
        if (status === "ok" && snapshot.loginExpiresAtMs > 0 && loginLeft < 3 * 86400000) {
            return loginLeft > 0 ? i18n("Login expires in %1", duration(loginLeft)) : i18n("Login expired");
        }
        if (status === "token_expired" && hasData) {
            return i18n("Data from %1", clock(snapshot.fetchedAtMs));
        }
        const next = snapshot.nextUpdateAtMs;
        if (next <= 0 || status === "logged_out" || status === "auth_error") {
            return "";
        }
        const left = next - now;
        if (status === "rate_limited" || status === "offline" || status === "error") {
            return left > 60000 ? i18n("Retry in %1", duration(left)) : i18n("Retrying soon");
        }
        return left > 60000 ? i18n("Next check in %1", duration(left)) : "";
    }

    // "positive", "neutral", "negative" or "muted"
    readonly property string footerTone: {
        switch (status) {
        case "ok": return "positive";
        case "rate_limited":
        case "offline":
        case "error": return "neutral";
        case "not_running":
        case "logged_out":
        case "auth_error": return "negative";
        }
        return "muted";
    }

    readonly property string tooltipText: {
        if (emptyState) {
            return emptyState.title;
        }
        const lines = [];
        for (const r of rows) {
            lines.push(r.leftText !== "" ? i18n("%1 %2 · %3", r.label, r.percentText, r.leftText.toLowerCase())
                                        : i18n("%1 %2", r.label, r.percentText));
        }
        lines.push(footerText);
        return lines.join("\n");
    }

    function findBar(kind: string): var {
        if (!snapshot) {
            return null;
        }
        for (const b of snapshot.bars) {
            if (b.kind === kind) {
                return b;
            }
        }
        return null;
    }

    function panelEntry(b: var, short: string): var {
        if (!b || !hasData || status === "logged_out" || status === "auth_error") {
            return { short: short, percent: 0, percentText: "–", level: 0 };
        }
        const reset = b.resetsAtMs > 0 && b.resetsAtMs <= now;
        const p = reset ? 0 : b.percent;
        return { short: short, percent: p, percentText: percentText(p), level: reset ? 0 : b.level };
    }

    function percentText(p: real): string {
        return i18nc("percentage", "%1%", Math.floor(p));
    }

    // Countdown such as "1 hr 12 min" or "6 days", matching the daemon's alerts.
    function duration(ms: real): string {
        if (ms < 60000) {
            return i18n("less than a minute");
        }
        const mins = Math.round(ms / 60000);
        const days = Math.floor(mins / 1440);
        const hours = Math.floor(mins / 60) % 24;
        const minutes = mins % 60;
        if (days >= 2) {
            return i18np("%1 day", "%1 days", days);
        }
        if (days === 1) {
            return hours > 0 ? i18n("1 day %1 hr", hours) : i18n("1 day");
        }
        if (hours > 0) {
            return minutes > 0 ? i18n("%1 hr %2 min", hours, minutes) : i18n("%1 hr", hours);
        }
        return i18n("%1 min", minutes);
    }

    function ago(ms: real): string {
        return ms < 60000 ? i18n("just now") : i18n("%1 ago", duration(ms));
    }

    // Reset time: "Today 3:20 PM", "Tomorrow 1:00 AM", "Tue 8:00 AM" or a date.
    function clock(ms: real): string {
        const d = new Date(ms);
        const today = new Date(now);
        today.setHours(0, 0, 0, 0);
        const day = new Date(ms);
        day.setHours(0, 0, 0, 0);
        const diffDays = Math.round((day - today) / 86400000);
        const time = d.toLocaleTimeString(Qt.locale(), Locale.ShortFormat);
        if (diffDays === 0) {
            return i18n("Today %1", time);
        }
        if (diffDays === 1) {
            return i18n("Tomorrow %1", time);
        }
        if (diffDays > 1 && diffDays < 7) {
            return Qt.locale().dayName(d.getDay(), Locale.ShortFormat) + " " + time;
        }
        return d.toLocaleDateString(Qt.locale(), Locale.ShortFormat) + " " + time;
    }

    function readSnapshot() {
        const raw = props.properties.Snapshot;
        // GetAll delivers a wrapped string and PropertiesChanged a plain one.
        const json = raw === undefined || raw === null ? "" : String(raw);
        if (json === "" || json === lastJson) {
            return;
        }
        try {
            const s = JSON.parse(json);
            if (s && s.v === 1) {
                lastJson = json;
                snapshot = s;
                now = Date.now();
            }
        } catch (e) {
            console.warn("clankerwatch: unreadable snapshot", e);
        }
    }

    // Asks the daemon for a fresh fetch. The daemon rate-limits these, and the
    // call also starts the daemon through D-Bus activation when it is down.
    function requestRefresh() {
        now = Date.now();
        DBus.SessionBus.asyncCall({ service: busName, path: objectPath, iface: busName, member: "Refresh" },
            () => {}, reply => console.warn("clankerwatch: Refresh failed:", reply.error.message));
    }

    function refreshIfAllowed() {
        now = Date.now();
        const at = snapshot ? snapshot.refreshAllowedAtMs : null;
        if (!serviceRunning || (at !== null && at !== undefined && now >= at)) {
            requestRefresh();
        }
    }

    readonly property DBus.Properties props: DBus.Properties {
        busType: DBus.BusType.Session
        service: model.busName
        path: model.objectPath
        iface: model.busName
        onPropertiesChanged: model.readSnapshot()
        onRefreshed: model.readSnapshot()
    }

    // The property binding does not re-read after a daemon restart on its own.
    readonly property DBus.DBusServiceWatcher watcher: DBus.DBusServiceWatcher {
        busType: DBus.BusType.Session
        watchedService: model.busName
        onRegisteredChanged: {
            if (registered) {
                model.props.updateAll();
            }
        }
    }

    readonly property Timer ticker: Timer {
        interval: 60000
        repeat: true
        running: true
        onTriggered: model.now = Date.now()
    }
}
