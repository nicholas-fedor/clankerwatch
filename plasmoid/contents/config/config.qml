// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

import QtQuick
import org.kde.plasma.configuration

// The widget's settings pages. The daemon owns the settings, so the page
// reads and writes them over D-Bus instead of the applet configuration.
ConfigModel {
    ConfigCategory {
        name: i18nc("@title:tab", "General")
        icon: "configure"
        source: "ConfigGeneral.qml"
    }
}
