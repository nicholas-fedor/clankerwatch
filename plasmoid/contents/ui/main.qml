// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

pragma ComponentBehavior: Bound

import QtQuick
import org.kde.kirigami as Kirigami
import org.kde.plasma.core as PlasmaCore
import org.kde.plasma.plasmoid

PlasmoidItem {
    id: root

    property UsageModel usage: UsageModel {}

    Plasmoid.icon: "speedometer"
    Plasmoid.contextualActions: [
        PlasmaCore.Action {
            text: i18nc("@action", "Refresh Now")
            icon.name: "view-refresh"
            onTriggered: root.usage.requestRefresh()
        },
        PlasmaCore.Action {
            text: i18nc("@action", "Open Usage Page")
            icon.name: "internet-web-browser-symbolic"
            onTriggered: Qt.openUrlExternally("https://claude.ai/settings/usage")
        }
    ]

    // Small desktop placements fall back to the compact view.
    switchWidth: Kirigami.Units.gridUnit * 12
    switchHeight: Kirigami.Units.gridUnit * 8

    toolTipMainText: i18n("Claude usage")
    toolTipSubText: usage.tooltipText
    toolTipTextFormat: Text.PlainText

    // Deferred so the popup finishes its first layout before data changes.
    onExpandedChanged: () => {
        if (root.expanded) {
            Qt.callLater(root.usage.refreshIfAllowed);
        }
    }

    compactRepresentation: CompactRepresentation {
        usage: root.usage
        plasmoidItem: root
    }

    fullRepresentation: FullRepresentation {
        usage: root.usage
    }
}
