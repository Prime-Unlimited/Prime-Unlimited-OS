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
    property string authToken: "" // injected by the trusted local launcher
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
                refreshInstalled()
            } catch (e) {
                statusText = "Invalid catalog response"
            }
        }
        request.onerror = function() { statusText = "Connection failed" }
        request.ontimeout = function() { statusText = "Connection timed out" }
        request.open("GET", apiBase + "/api/v1/mods")
        request.send()
    }

    function getJson(path, done) {
        var request = new XMLHttpRequest()
        request.timeout = 4000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            if (request.status === 200) done(JSON.parse(request.responseText))
            else statusText = "Offline: showing cached installed state"
        }
        request.onerror = function() { statusText = "Offline: installed mods remain available" }
        request.open("GET", apiBase + path); request.send()
    }

    function refreshInstalled() {
        getJson("/api/v1/installed", function(payload) { installed = payload.mods || ({}) })
        getJson("/api/v1/transactions", function(payload) { transactions = payload.transactions || [] })
    }

    function mutate(action, id, version) {
        if (!authToken) { statusText = "Changes disabled: local authorization unavailable"; return }
        var request = new XMLHttpRequest(); request.timeout = 30000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            try {
                var payload = JSON.parse(request.responseText)
                statusText = request.status >= 200 && request.status < 300 ?
                    (payload.transactionId ? "Transaction " + payload.transactionId + " started" : "Change completed") :
                    (payload.error || "Change failed")
            } catch (e) { statusText = "Invalid service response" }
            refreshInstalled()
        }
        request.open("POST", apiBase + "/api/v1/" + action)
        request.setRequestHeader("Content-Type", "application/json")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        var data = {"id": id}; if (version) data.version = "=" + version
        request.send(JSON.stringify(data))
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
            model: ["AVAILABLE", "INSTALLED", "UPDATES"]
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
    Flickable {
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
                model: modLoader.mods.filter(function(item) {
                    if (selectedTab === 1) return installed[item.id] !== undefined
                    if (selectedTab === 2) return installed[item.id] !== undefined && installed[item.id].version !== item.version
                    return true
                })
                delegate: Rectangle {
                    width: entries.width
                    height: 86
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
                        text: modelData.compatible ? "Compatible" : "Not compatible with this device"
                        color: modelData.compatible ? "#73d6a3" : "#ff9b78"
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
                        MouseArea { anchors.fill: parent; onClicked: {
                            if (!parent.present) mutate("install", modelData.id, modelData.version)
                            else mutate(installed[modelData.id].enabled ? "disable" : "enable", modelData.id, "")
                        }}
                    }
                    Rectangle {
                        visible: installed[modelData.id] !== undefined
                        width: 104; height: 48; radius: 9; x: parent.width - 110; y: 19
                        color: "#4a2a2a"
                        Text { anchors.centerIn: parent; text: "REMOVE"; color: "#ffb29a"; font.bold: true }
                        MouseArea { anchors.fill: parent; onClicked: pendingUninstall = modelData.id }
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
                        var target = pendingUninstall; pendingUninstall = ""; mutate("uninstall", target, "")
                    }}
                }
            }
        }
    }
}
