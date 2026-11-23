import QtQuick 2.12
import Planck 1.0

Item {
    id: root
    anchors.fill: parent
    property var snapshots: []
    property string status: "Deck data unavailable"

    function value(path, field, fallback) {
        try {
            var p = Planck.getProperty(path)
            if (!p || !p.translator) return fallback
            var t = p.translator
            if (typeof t[field] !== "undefined" && t[field] !== null) return t[field]
        } catch (e) {}
        return fallback
    }

    function numberValue(path) {
        var raw = value(path, "unnormalized", null)
        if (raw === null) raw = value(path, "value", null)
        if (raw === null || raw === "") return null
        var n = Number(raw)
        return isFinite(n) ? n : null
    }

    function displayNumber(n) {
        return n === null ? "?" : Number(n).toFixed(1)
    }

    function refresh() {
        var active = []
        try {
            var p = Planck.getProperty("/GUI/Decks/ActiveDecks")
            if (p && p.translator && p.translator.entries) active = p.translator.entries
        } catch (e) {}
        var next = []
        for (var i = 0; i < active.length && i < 4; ++i) {
            var n = Number(active[i])
            if (!isFinite(n) || n < 1 || n > 4) continue
            var base = "/Engine/Deck" + n
            next.push({
                deck: n,
                title: value(base + "/Track/TrackName", "string", ""),
                artist: value(base + "/Track/ArtistName", "string", ""),
                bpm: numberValue(base + "/Track/CurrentBPM"),
                position: numberValue(base + "/Track/SongPosition"),
                length: numberValue(base + "/Track/TrackLength"),
                playState: value(base + "/PlayState", "state", null)
            })
        }
        snapshots = next
        status = next.length ? "Local deck status" : "No active deck data"
    }

    Timer {
        interval: 500
        repeat: true
        running: root.visible && (typeof planckQuickView === "undefined" || planckQuickView.isMainScreen)
        triggeredOnStart: true
        onTriggered: root.refresh()
    }

    Rectangle {
        width: 300
        height: Math.min(220, 38 + column.implicitHeight)
        x: Math.max(0, root.width - width - 8)
        y: 8
        radius: 6
        color: "#e6191d22"
        border.color: "#59616a"
        Column {
            id: column
            anchors.fill: parent
            anchors.margins: 8
            spacing: 4
            Text { text: root.status; color: "#f2f4f5"; font.bold: true }
            Repeater {
                model: root.snapshots
                delegate: Column {
                    width: column.width
                    Text {
                        width: parent.width
                        color: "#cbd6e3"
                        elide: Text.ElideRight
                        text: "Deck " + modelData.deck + ": " + (modelData.artist || "?") + " — " + (modelData.title || "?")
                    }
                    Text {
                        color: "#9db0c5"
                        text: "BPM " + root.displayNumber(modelData.bpm)
                            + "   Position " + root.displayNumber(modelData.position)
                            + "   State " + (modelData.playState === null ? "?" : String(modelData.playState))
                    }
                }
            }
        }
    }
}
