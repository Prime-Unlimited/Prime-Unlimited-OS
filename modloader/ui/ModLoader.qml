import QtQuick 2.12

Rectangle {
    id: modLoader
    width: 800
    height: 480
    color: "#101215"

    property string apiBase: "http://127.0.0.1:8098"
    property var mods: []
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
            } catch (e) {
                statusText = "Invalid catalog response"
            }
        }
        request.onerror = function() { statusText = "Connection failed" }
        request.ontimeout = function() { statusText = "Connection timed out" }
        request.open("GET", apiBase + "/api/v1/mods")
        request.send()
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
                model: modLoader.mods
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
                }
            }
        }
    }
}
