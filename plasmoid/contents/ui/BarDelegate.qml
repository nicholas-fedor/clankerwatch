// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

import QtQuick
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.plasma.components as PlasmaComponents3

// One limit: label and percent, the bar, then the reset countdown and time.
ColumnLayout {
    id: row

    property string label
    property string percentText
    property real percent
    property int level
    property string leftText
    property string rightText

    spacing: Kirigami.Units.smallSpacing

    RowLayout {
        Layout.fillWidth: true
        spacing: Kirigami.Units.largeSpacing

        PlasmaComponents3.Label {
            Layout.fillWidth: true
            text: row.label
            textFormat: Text.PlainText
            elide: Text.ElideRight
        }
        PlasmaComponents3.Label {
            text: row.percentText
            textFormat: Text.PlainText
            font.weight: Font.DemiBold
            font.features: { "tnum": 1 }
            color: row.level >= 2 ? Kirigami.Theme.negativeTextColor
                 : row.level === 1 ? Kirigami.Theme.neutralTextColor
                 : Kirigami.Theme.textColor
        }
    }

    UsageBar {
        Layout.fillWidth: true
        percent: row.percent
        level: row.level
    }

    RowLayout {
        Layout.fillWidth: true
        spacing: Kirigami.Units.largeSpacing
        visible: row.leftText !== "" || row.rightText !== ""

        PlasmaComponents3.Label {
            Layout.fillWidth: true
            text: row.leftText
            textFormat: Text.PlainText
            elide: Text.ElideRight
            font: Kirigami.Theme.smallFont
            color: Kirigami.Theme.disabledTextColor
        }
        PlasmaComponents3.Label {
            visible: text !== ""
            text: row.rightText
            textFormat: Text.PlainText
            font: Kirigami.Theme.smallFont
            color: Kirigami.Theme.disabledTextColor
        }
    }
}
