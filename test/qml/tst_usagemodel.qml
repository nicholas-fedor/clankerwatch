// Copyright (c) Nicholas Fedor 2026 <nick@nickfedor.com>
// SPDX-License-Identifier: AGPL-3.0-or-later

import QtQuick
import QtTest
import "../../plasmoid/contents/ui"

// Covers which limits the panel shows. No daemon runs here, so the model reports
// the service as not running and keeps showing the snapshot it was given.
Item {
    id: root

    // Plasma provides these to a widget at runtime. Substituting the arguments
    // is all the model's values need.
    function i18n(text, ...args) {
        return args.reduce((out, arg, index) => out.replace("%" + (index + 1), arg), text);
    }

    function i18nc(context, text, ...args) {
        return i18n(text, ...args);
    }

    function i18np(singular, plural, count, ...args) {
        return i18n(count === 1 ? singular : plural, count, ...args);
    }

    Component {
        id: modelComponent
        UsageModel {}
    }

    TestCase {
        name: "UsageModelPanelBars"

        // Builds one bar of a snapshot.
        function bar(short, percent, credit) {
            return { short: short, label: short, percent: percent, level: 0, resetsAtMs: 0, credit: credit === true };
        }

        // Returns a model holding a snapshot with the given bars.
        function modelWith(bars, fetchedAtMs) {
            const model = createTemporaryObject(modelComponent, root);
            verify(model !== null);
            model.snapshot = { status: "ok", plan: "", fetchedAtMs: fetchedAtMs, bars: bars };
            return model;
        }

        function shorts(model) {
            return model.panelBars.map(b => b.short);
        }

        function test_withSessionLimit() {
            const model = modelWith([bar("5h", 66), bar("7d", 17), bar("Fable", 0)], Date.now());
            compare(shorts(model), ["5h", "7d"]);
        }

        function test_withoutSessionLimit() {
            const model = modelWith([bar("7d", 49), bar("Fable", 8)], Date.now());
            compare(shorts(model), ["7d", "Fable"]);
            compare(model.panelBars[0].percentText, "49%");
            compare(model.panelBars[1].percentText, "8%");
        }

        function test_creditNeverTakesARow() {
            const model = modelWith([bar("Credit", 64, true), bar("7d", 49)], Date.now());
            compare(shorts(model), ["7d"]);
        }

        function test_onlyCreditShowsThePlaceholder() {
            const model = modelWith([bar("Credit", 64, true)], Date.now());
            compare(shorts(model), ["7d"]);
            compare(model.panelBars[0].percentText, "–");
        }

        function test_noDataShowsThePlaceholder() {
            const model = modelWith([bar("5h", 66), bar("7d", 17)], 0);
            compare(shorts(model), ["7d"]);
            compare(model.panelBars[0].percentText, "–");
        }

        function test_passedResetShowsZero() {
            const passed = bar("7d", 49);
            passed.resetsAtMs = Date.now() - 60000;
            passed.level = 2;
            const model = modelWith([passed], Date.now());
            compare(model.panelBars[0].percent, 0);
            compare(model.panelBars[0].level, 0);
        }
    }
}
