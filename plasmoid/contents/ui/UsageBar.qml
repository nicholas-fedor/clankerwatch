// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

import QtQuick
import org.kde.kirigami as Kirigami

// A rounded track with a fill colored by level: accent, warning or critical.
Item {
    id: bar

    property real percent: 0
    property int level: 0

    implicitWidth: Kirigami.Units.gridUnit * 4
    implicitHeight: Math.round(Kirigami.Units.smallSpacing * 1.5)

    readonly property real fraction: Math.max(0, Math.min(1, percent / 100))

    Rectangle {
        anchors.fill: parent
        radius: height / 2
        color: Kirigami.Theme.textColor
        opacity: 0.15
    }

    Rectangle {
        height: parent.height
        width: bar.fraction <= 0 ? 0 : Math.max(height, Math.round(parent.width * bar.fraction))
        visible: width > 0
        radius: height / 2
        color: bar.level >= 2 ? Kirigami.Theme.negativeTextColor
             : bar.level === 1 ? Kirigami.Theme.neutralTextColor
             : Kirigami.Theme.highlightColor
    }
}
