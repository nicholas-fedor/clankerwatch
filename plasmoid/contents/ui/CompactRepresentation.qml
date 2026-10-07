// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import org.kde.plasma.components as PlasmaComponents3
import org.kde.plasma.core as PlasmaCore
import org.kde.plasma.plasmoid

// Panel view: "5h ▕bar▏ 66%" over "7d ▕bar▏ 17%". The font follows the panel
// thickness, and a vertical panel stacks percent over bar.
MouseArea {
    id: compact

    required property UsageModel usage
    required property PlasmoidItem plasmoidItem

    readonly property bool vertical: Plasmoid.formFactor === PlasmaCore.Types.Vertical
    // Two rows share the height. A row may be a little shorter than the font's
    // line box, because glyphs never use the full box.
    readonly property real rowFit: 0.9
    readonly property bool defaultFits: height >= 2 * rowFit * defaultMetrics.height
    readonly property bool twoRows: height >= 2 * rowFit * smallMetrics.height
    readonly property int rowHeight: Math.floor(height / 2)
    readonly property font rowFont: defaultFits ? Kirigami.Theme.defaultFont : Kirigami.Theme.smallFont
    readonly property int barWidth: Math.round(Kirigami.Units.gridUnit * (defaultFits ? 4 : 3.5))
    readonly property string summary: usage.panelBars.map(b => b.short + " " + b.percentText).join(", ")

    acceptedButtons: Qt.LeftButton | Qt.MiddleButton
    hoverEnabled: true
    opacity: usage.isStale ? 0.6 : 1

    Accessible.name: i18n("Claude usage: %1", summary)
    Accessible.role: Accessible.Button

    Layout.minimumWidth: vertical ? -1 : layout.implicitWidth
    Layout.preferredWidth: vertical ? -1 : layout.implicitWidth
    Layout.minimumHeight: vertical ? layout.implicitHeight : -1
    Layout.preferredHeight: vertical ? layout.implicitHeight : -1

    onClicked: mouse => {
        if (mouse.button === Qt.MiddleButton) {
            usage.requestRefresh();
        } else {
            plasmoidItem.expanded = !plasmoidItem.expanded;
        }
    }

    FontMetrics {
        id: defaultMetrics
        font: Kirigami.Theme.defaultFont
    }
    FontMetrics {
        id: smallMetrics
        font: Kirigami.Theme.smallFont
    }
    TextMetrics {
        id: labelMetrics
        font: compact.rowFont
        text: "7d"
    }
    TextMetrics {
        id: percentMetrics
        font: compact.rowFont
        text: "100%"
    }

    GridLayout {
        id: layout

        anchors.centerIn: compact.vertical ? undefined : parent
        anchors.left: compact.vertical ? parent.left : undefined
        anchors.right: compact.vertical ? parent.right : undefined
        anchors.verticalCenter: compact.vertical ? parent.verticalCenter : undefined
        anchors.margins: compact.vertical ? Kirigami.Units.smallSpacing : 0

        flow: compact.vertical || compact.twoRows ? GridLayout.TopToBottom : GridLayout.LeftToRight
        rowSpacing: compact.vertical ? Kirigami.Units.smallSpacing * 2 : 0
        columnSpacing: Kirigami.Units.largeSpacing

        // Keyed by count, so value changes never rebuild the delegates.
        Repeater {
            model: compact.usage.panelBars.length

            delegate: Loader {
                id: entry
                required property int index
                readonly property var bar: compact.usage.panelBars[index] ?? ({ short: "", percent: 0, percentText: "", level: 0 })
                Layout.fillWidth: compact.vertical
                Layout.preferredHeight: compact.twoRows && !compact.vertical ? compact.rowHeight : -1
                sourceComponent: compact.vertical ? verticalEntry : horizontalEntry

                Component {
                    id: horizontalEntry
                    RowLayout {
                        spacing: Kirigami.Units.smallSpacing
                        PlasmaComponents3.Label {
                            Layout.preferredWidth: Math.ceil(labelMetrics.advanceWidth)
                            text: entry.bar.short
                            font: compact.rowFont
                            color: Kirigami.Theme.disabledTextColor
                            textFormat: Text.PlainText
                        }
                        UsageBar {
                            Layout.preferredWidth: compact.barWidth
                            Layout.preferredHeight: Kirigami.Units.smallSpacing
                            Layout.alignment: Qt.AlignVCenter
                            percent: entry.bar.percent
                            level: entry.bar.level
                        }
                        PlasmaComponents3.Label {
                            Layout.preferredWidth: Math.ceil(percentMetrics.advanceWidth)
                            horizontalAlignment: Text.AlignRight
                            text: entry.bar.percentText
                            font: compact.rowFont
                            textFormat: Text.PlainText
                            color: compact.levelColor(entry.bar.level)
                        }
                    }
                }

                Component {
                    id: verticalEntry
                    ColumnLayout {
                        spacing: Kirigami.Units.smallSpacing
                        PlasmaComponents3.Label {
                            Layout.fillWidth: true
                            horizontalAlignment: Text.AlignHCenter
                            fontSizeMode: Text.HorizontalFit
                            minimumPixelSize: 6
                            text: entry.bar.percentText
                            font: Kirigami.Theme.smallFont
                            textFormat: Text.PlainText
                            color: compact.levelColor(entry.bar.level)
                        }
                        UsageBar {
                            Layout.fillWidth: true
                            Layout.preferredHeight: Kirigami.Units.smallSpacing
                            percent: entry.bar.percent
                            level: entry.bar.level
                        }
                    }
                }
            }
        }
    }

    function levelColor(level: int): color {
        return level >= 2 ? Kirigami.Theme.negativeTextColor
             : level === 1 ? Kirigami.Theme.neutralTextColor
             : Kirigami.Theme.textColor;
    }
}
