// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.plasma.components as PlasmaComponents3

// The popup, and the widget itself when it sits on the desktop.
//
// The design measures its padding from the popup's outer edge: 16 px at the
// sides, 8 px at the top, and 10 px at the bottom. Plasma's dialog frame
// already insets the content by smallSpacing, so the widget adds the rest.
Item {
    id: full

    required property UsageModel usage

    readonly property real sidePadding: Kirigami.Units.largeSpacing + Kirigami.Units.smallSpacing
    readonly property real topPadding: Kirigami.Units.smallSpacing
    readonly property real bottomPadding: Kirigami.Units.smallSpacing + 2

    implicitWidth: content.implicitWidth + sidePadding * 2
    implicitHeight: content.implicitHeight + topPadding + bottomPadding
    Layout.preferredWidth: Kirigami.Units.gridUnit * 22 + sidePadding * 2
    Layout.minimumWidth: Kirigami.Units.gridUnit * 16 + sidePadding * 2
    Layout.minimumHeight: implicitHeight
    Layout.preferredHeight: implicitHeight

    ColumnLayout {
        id: content

        anchors.fill: parent
        anchors.leftMargin: full.sidePadding
        anchors.rightMargin: full.sidePadding
        anchors.topMargin: full.topPadding
        anchors.bottomMargin: full.bottomPadding
        spacing: Kirigami.Units.largeSpacing

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.largeSpacing

            Kirigami.Heading {
                level: 4
                text: i18n("Claude usage")
                font.weight: Font.DemiBold
                textFormat: Text.PlainText
            }
            Rectangle {
                visible: full.usage.plan !== "" && !full.usage.emptyState
                implicitWidth: planLabel.implicitWidth + Kirigami.Units.largeSpacing * 2
                implicitHeight: planLabel.implicitHeight + 2
                radius: height / 2
                color: "transparent"
                border.width: 1
                border.color: Qt.alpha(Kirigami.Theme.textColor, 0.2)

                PlasmaComponents3.Label {
                    id: planLabel
                    anchors.centerIn: parent
                    text: full.usage.plan
                    textFormat: Text.PlainText
                    font: Kirigami.Theme.smallFont
                    color: Kirigami.Theme.disabledTextColor
                }
            }
            Item {
                Layout.fillWidth: true
            }
            PlasmaComponents3.ToolButton {
                icon.name: "view-refresh"
                display: PlasmaComponents3.AbstractButton.IconOnly
                text: i18nc("@action:button", "Refresh")
                onClicked: full.usage.requestRefresh()
                PlasmaComponents3.ToolTip.text: text
                PlasmaComponents3.ToolTip.visible: hovered
            }
            PlasmaComponents3.ToolButton {
                icon.name: "internet-web-browser-symbolic"
                display: PlasmaComponents3.AbstractButton.IconOnly
                text: i18nc("@action:button", "Open the claude.ai usage page")
                onClicked: Qt.openUrlExternally("https://claude.ai/settings/usage")
                PlasmaComponents3.ToolTip.text: text
                PlasmaComponents3.ToolTip.visible: hovered
            }
        }

        Kirigami.Separator {
            Layout.fillWidth: true
        }

        // Each row takes an equal share of any spare height, so rows spread out
        // evenly when the widget is resized taller than its content.
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            visible: !full.usage.emptyState
            spacing: Kirigami.Units.largeSpacing + Kirigami.Units.smallSpacing

            // Keyed by count: delegates survive value changes and are only rebuilt
            // when rows are added or removed, never during a layout pass.
            Repeater {
                model: full.usage.rows.length
                delegate: Item {
                    id: slot
                    required property int index
                    readonly property var row: full.usage.rows[index] ?? null
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    Layout.minimumHeight: delegate.implicitHeight
                    implicitHeight: delegate.implicitHeight

                    BarDelegate {
                        id: delegate
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        opacity: full.usage.dimBars ? 0.6 : 1
                        label: slot.row ? slot.row.label : ""
                        percent: slot.row ? slot.row.percent : 0
                        percentText: slot.row ? slot.row.percentText : ""
                        level: slot.row ? slot.row.level : 0
                        leftText: slot.row ? slot.row.leftText : ""
                        rightText: slot.row ? slot.row.rightText : ""
                    }
                }
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.minimumHeight: Kirigami.Units.gridUnit * 7
            visible: !!full.usage.emptyState
            spacing: Kirigami.Units.smallSpacing

            Item {
                Layout.fillHeight: true
            }
            PlasmaComponents3.Label {
                Layout.fillWidth: true
                horizontalAlignment: Text.AlignHCenter
                text: full.usage.emptyState ? full.usage.emptyState.title : ""
                textFormat: Text.PlainText
                font.weight: Font.DemiBold
                wrapMode: Text.Wrap
            }
            PlasmaComponents3.Label {
                Layout.fillWidth: true
                Layout.leftMargin: Kirigami.Units.gridUnit
                Layout.rightMargin: Kirigami.Units.gridUnit
                visible: text !== ""
                horizontalAlignment: Text.AlignHCenter
                text: full.usage.emptyState ? full.usage.emptyState.body : ""
                textFormat: Text.PlainText
                wrapMode: Text.Wrap
                font: Kirigami.Theme.smallFont
                color: Kirigami.Theme.disabledTextColor
            }
            PlasmaComponents3.Button {
                Layout.alignment: Qt.AlignHCenter
                Layout.topMargin: Kirigami.Units.smallSpacing
                visible: !!full.usage.emptyState && full.usage.emptyState.button !== ""
                text: full.usage.emptyState ? full.usage.emptyState.button : ""
                onClicked: full.usage.requestRefresh()
            }
            Item {
                Layout.fillHeight: true
            }
        }

        Kirigami.Separator {
            Layout.fillWidth: true
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing * 1.5

            Rectangle {
                implicitWidth: 6
                implicitHeight: 6
                radius: 3
                color: {
                    switch (full.usage.footerTone) {
                    case "positive": return Kirigami.Theme.positiveTextColor;
                    case "neutral": return Kirigami.Theme.neutralTextColor;
                    case "negative": return Kirigami.Theme.negativeTextColor;
                    }
                    return Kirigami.Theme.disabledTextColor;
                }
            }
            PlasmaComponents3.Label {
                Layout.fillWidth: true
                text: full.usage.footerText
                textFormat: Text.PlainText
                elide: Text.ElideRight
                font: Kirigami.Theme.smallFont
                color: Kirigami.Theme.disabledTextColor
            }
            PlasmaComponents3.Label {
                visible: text !== ""
                text: full.usage.footerRight
                textFormat: Text.PlainText
                font: Kirigami.Theme.smallFont
                color: Kirigami.Theme.disabledTextColor
            }
        }
    }
}
