import QtQuick 2.12

Rectangle {
    id: modLoader
    width: 800
    height: 480
    color: "#101215"

    property string apiBase: "http://127.0.0.1:8098"
    property var mods: []
    property var installed: ({})
    property var transactions: []
    property var safety: ({"safeMode": false, "mods": ({})})
    property string authToken: "__PRIME_UNLIMITED_AUTH_TOKEN__"
    property bool busy: false
    property int selectedTab: 0
    property string pendingUninstall: ""
    property string statusText: "Connecting..."
    property string deviceText: "Device not detected"
    signal closeRequested()

    function refresh() {
        var request = new XMLHttpRequest()
        request.timeout = 4000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            if (request.status !== 200) {
                statusText = "Catalog unavailable"
                return
            }
            try {
                var payload = JSON.parse(request.responseText)
                mods = payload.mods || []
                deviceText = payload.model + " / " + payload.firmware
                statusText = mods.length + " mods in catalog"
                refreshState()
            } catch (e) {
                statusText = "Invalid catalog response"
            }
        }
        request.onerror = function() { statusText = "Connection failed" }
        request.ontimeout = function() { statusText = "Connection timed out" }
        request.open("GET", apiBase + "/api/v1/mods")
        request.send()
    }

    function getJson(path, done, quiet) {
        var request = new XMLHttpRequest()
        request.timeout = 4000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            if (request.status === 200) {
                try { done(JSON.parse(request.responseText)) }
                catch (e) { if (!quiet) statusText = "Invalid service response" }
            } else if (!quiet) statusText = "Offline: showing cached installed state"
        }
        request.onerror = function() { if (!quiet) statusText = "Offline: installed mods remain available" }
        request.open("GET", apiBase + path); request.send()
    }

    function refreshState() {
        getJson("/api/v1/installed", function(payload) { installed = payload.mods || ({}) })
        getJson("/api/v1/transactions", function(payload) { transactions = payload.transactions || [] })
        getJson("/api/v1/safety", function(payload) { safety = payload }, true)
    }

    function mutation(action, id, version, done) {
        if (!authToken) { statusText = "Changes disabled: local authorization unavailable"; return }
        var request = new XMLHttpRequest(); request.timeout = 30000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            try {
                var payload = JSON.parse(request.responseText)
                var ok = request.status >= 200 && request.status < 300
                statusText = ok ? (payload.transactionId ? "Transaction " + payload.transactionId + " completed" : "Change completed") : (payload.error || "Change failed")
                busy = false; refreshState(); if (done) done(ok, payload)
            } catch (e) { busy = false; statusText = "Invalid service response"; if (done) done(false, ({})) }
        }
        request.onerror = function() { busy = false; statusText = "Connection failed"; if (done) done(false, ({})) }
        busy = true
        request.open("POST", apiBase + "/api/v1/" + action)
        request.setRequestHeader("Content-Type", "application/json")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        if (action === "safe-mode/enable" || action === "safe-mode/disable") request.send()
        else request.send(JSON.stringify({"id": id}))
    }

    function installVerified(id) {
        var steps = ["cache", "stage", "inspect", "install"]
        function next(index) {
            if (index >= steps.length) { statusText = "Verified installation completed"; refreshState(); return }
            statusText = "Installation: " + steps[index]
            mutation(steps[index], id, "", function(ok) { if (ok) next(index + 1) })
        }
        next(0)
    }

    function visibleMods() {
        if (selectedTab === 0) return mods
        var result = []
        var seen = ({})
        for (var i = 0; i < mods.length; ++i) {
            if (installed[mods[i].id] !== undefined) { result.push(mods[i]); seen[mods[i].id] = true }
        }
        var ids = Object.keys(installed).sort()
        for (var j = 0; j < ids.length; ++j) {
            if (!seen[ids[j]]) result.push({"id": ids[j], "name": ids[j] + " (not in catalog)", "version": installed[ids[j]].version, "compatible": true, "dependencies": [], "libraries": []})
        }
        return result
    }

    Component.onCompleted: refresh()

    Rectangle {
        id: header
        width: parent.width
        height: 86
        color: "#191c20"
        Text {
            anchors.left: parent.left
            anchors.leftMargin: 24
            anchors.verticalCenter: parent.verticalCenter
            text: "PRIME UNLIMITED  MOD LOADER"
            color: "#f4f2ee"
            font.pixelSize: 27
            font.bold: true
        }
        Rectangle {
            width: 94; height: 54; radius: 12
            anchors.right: parent.right
            anchors.rightMargin: 18
            anchors.verticalCenter: parent.verticalCenter
            color: "#33373c"
            Text { anchors.centerIn: parent; text: "CLOSE"; color: "white"; font.pixelSize: 17 }
            MouseArea { anchors.fill: parent; onClicked: modLoader.closeRequested() }
        }
    }
    Text {
        id: device
        x: 24; y: 103
        text: deviceText
        color: "#aab0b8"
        font.pixelSize: 17
    }
    Row {
        x: 330; y: 101; spacing: 8
        Repeater {
            model: ["AVAILABLE", "INSTALLED", "ACTIVITY"]
            delegate: Rectangle {
                width: 116; height: 40; radius: 9
                color: selectedTab === index ? "#ff541c" : "#33373c"
                Text { anchors.centerIn: parent; text: modelData; color: "white"; font.pixelSize: 13; font.bold: true }
                MouseArea { anchors.fill: parent; onClicked: selectedTab = index }
            }
        }
    }
    Text {
        id: status
        x: 24; y: 133
        text: statusText
        color: "#ff541c"
        font.pixelSize: 16
    }
    Rectangle {
        x: 600; y: 140; width: 180; height: 28; radius: 8
        color: safety.safeMode ? "#8d2d21" : "#264536"
        Text { anchors.centerIn: parent; color: "white"; font.pixelSize: 13; text: safety.safeMode ? "SAFE MODE ON" : "SAFE MODE OFF" }
        MouseArea { anchors.fill: parent; enabled: !busy; onClicked: mutation(safety.safeMode ? "safe-mode/disable" : "safe-mode/enable", "", "", null) }
    }
    Flickable {
        visible: selectedTab !== 2
        x: 20; y: 174
        width: parent.width - 40
        height: parent.height - 190
        clip: true
        contentHeight: entries.height
        Column {
            id: entries
            width: parent.width
            spacing: 10
            Repeater {
                model: visibleMods()
                delegate: Rectangle {
                    width: entries.width
                    height: 116
                    radius: 12
                    color: "#22262b"
                    Text {
                        x: 16; y: 12
                        text: modelData.name + "  v" + modelData.version
                        color: "#f4f2ee"
                        font.pixelSize: 21
                        font.bold: true
                    }
                    Text {
                        x: 16; y: 48
                        text: safety.mods && safety.mods[modelData.id] && safety.mods[modelData.id].quarantined ? "Quarantined after " + safety.mods[modelData.id].failureCount + " failures" : (modelData.compatible ? "Compatible" : "Not compatible with this device")
                        color: safety.mods && safety.mods[modelData.id] && safety.mods[modelData.id].quarantined ? "#ff9b78" : (modelData.compatible ? "#73d6a3" : "#ff9b78")
                        font.pixelSize: 16
                    }
                    Text {
                        x: 330; y: 13; width: 250
                        text: (modelData.dependencies || []).length + " mod dependencies, " + (modelData.libraries || []).length + " libraries"
                        color: "#aab0b8"; font.pixelSize: 13; elide: Text.ElideRight
                    }
                    Rectangle {
                        width: 104; height: 48; radius: 9; x: parent.width - (present ? 224 : 116); y: 19
                        color: "#ff541c"
                        property bool present: installed[modelData.id] !== undefined
                        Text { anchors.centerIn: parent; color: "white"; font.bold: true
                            text: parent.present ? (installed[modelData.id].enabled ? "DISABLE" : "ENABLE") : "INSTALL" }
                        MouseArea { anchors.fill: parent; enabled: !busy; onClicked: {
                            if (!parent.present) installVerified(modelData.id)
                            else mutation(installed[modelData.id].enabled ? "disable" : "enable", modelData.id, "", null)
                        }}
                    }
                    Rectangle {
                        visible: installed[modelData.id] !== undefined
                        width: 104; height: 48; radius: 9; x: parent.width - 110; y: 19
                        color: "#4a2a2a"
                        Text { anchors.centerIn: parent; text: "REMOVE"; color: "#ffb29a"; font.bold: true }
                        MouseArea { anchors.fill: parent; enabled: !busy; onClicked: pendingUninstall = modelData.id }
                    }
                    Row {
                        visible: installed[modelData.id] !== undefined
                        x: 330; y: 78; spacing: 8
                        Rectangle {
                            width: 104; height: 28; radius: 7; color: "#34516b"
                            Text { anchors.centerIn: parent; text: "LAUNCH"; color: "white"; font.bold: true }
                            MouseArea { anchors.fill: parent; enabled: installed[modelData.id] && installed[modelData.id].enabled && !busy; onClicked: mutation("launch-set", modelData.id, "", null) }
                        }
                        Rectangle {
                            visible: safety.mods && safety.mods[modelData.id] && safety.mods[modelData.id].quarantined
                            width: 104; height: 28; radius: 7; color: "#6d512c"
                            Text { anchors.centerIn: parent; text: "RECOVER"; color: "white"; font.bold: true }
                            MouseArea { anchors.fill: parent; enabled: !busy; onClicked: mutation("recover", modelData.id, "", null) }
                        }
                    }
                }
            }
        }
    }
    Rectangle {
        visible: pendingUninstall !== ""
        anchors.fill: parent; color: "#d9101215"; z: 10
        Rectangle {
            width: 500; height: 190; radius: 14; anchors.centerIn: parent; color: "#22262b"
            Text { x: 24; y: 25; width: 452; wrapMode: Text.WordWrap; color: "white"; font.pixelSize: 20
                text: "Uninstall " + pendingUninstall + "? Persistent user data will be preserved." }
            Row { anchors.horizontalCenter: parent.horizontalCenter; y: 116; spacing: 18
                Rectangle { width: 160; height: 52; radius: 10; color: "#33373c"
                    Text { anchors.centerIn: parent; text: "CANCEL"; color: "white" }
                    MouseArea { anchors.fill: parent; onClicked: pendingUninstall = "" }
                }
                Rectangle { width: 160; height: 52; radius: 10; color: "#b43b22"
                    Text { anchors.centerIn: parent; text: "UNINSTALL"; color: "white"; font.bold: true }
                    MouseArea { anchors.fill: parent; onClicked: {
                        var target = pendingUninstall; pendingUninstall = ""
                        if (installed[target] && installed[target].enabled)
                            mutation("disable", target, "", function(ok) { if (ok) mutation("uninstall", target, "", null) })
                        else mutation("uninstall", target, "", null)
                    }}
                }
            }
        }
    }
    Flickable {
        visible: selectedTab === 2
        x: 20; y: 174; width: parent.width - 40; height: parent.height - 190
        clip: true; contentHeight: activityEntries.height
        Column {
            id: activityEntries; width: parent.width; spacing: 8
            Repeater {
                model: transactions
                delegate: Rectangle {
                    width: activityEntries.width; height: 58; radius: 9; color: "#22262b"
                    Text { x: 14; y: 8; color: "white"; font.pixelSize: 16; text: modelData.operation.toUpperCase() + "  " + modelData.modId + "  v" + modelData.version }
                    Text { x: 14; y: 33; color: modelData.status === "completed" ? "#73d6a3" : "#ff9b78"; font.pixelSize: 13; text: modelData.status + " / " + modelData.stage + " / " + modelData.id }
                }
            }
            Text { visible: transactions.length === 0; color: "#aab0b8"; font.pixelSize: 18; text: "No lifecycle transactions yet" }
        }
    }
}
