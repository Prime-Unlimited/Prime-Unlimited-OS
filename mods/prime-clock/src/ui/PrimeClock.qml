import QtQuick 2.12
import "../common/Stopwatch.js" as Stopwatch

Item {
    id: overlay
    anchors.fill: parent
    signal positionSaveRequested(real x, real y)
    property var stopwatch: Stopwatch.fresh()
    property real monotonicNow: 0
    property bool stopwatchMode: false
    property bool dragging: false
    property bool suppressClick: false
    property string clockText: Stopwatch.formatClock(new Date())

    function clampX(value) { return Math.max(0, Math.min(value, width - panel.width)) }
    function clampY(value) { return Math.max(0, Math.min(value, height - panel.height)) }
    function restorePosition(savedX, savedY) {
        if (savedX >= 0 && savedY >= 0) { panel.x = clampX(savedX); panel.y = clampY(savedY) }
        else { panel.x = clampX(width - panel.width - 180); panel.y = 6 }
    }
    function fetchMonotonic(callback) {
        var request = new XMLHttpRequest()
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE || request.status !== 200) return
            try { monotonicNow = JSON.parse(request.responseText).milliseconds; if (callback) callback() } catch (e) {}
        }
        request.open("GET", "http://127.0.0.1:8098/api/v1/monotonic"); request.send()
    }

    onWidthChanged: if (panel.x + panel.width > width) panel.x = clampX(panel.x)
    onHeightChanged: if (panel.y + panel.height > height) panel.y = clampY(panel.y)
    Component.onCompleted: restorePosition(-1, -1)

    Timer { interval: 1000; repeat: true; running: true; triggeredOnStart: true; onTriggered: overlay.clockText = Stopwatch.formatClock(new Date()) }
    Timer { interval: 100; repeat: true; running: overlay.stopwatch.running; onTriggered: overlay.fetchMonotonic(null) }

    Rectangle {
        id: panel
        width: stopwatchMode ? 260 : 92
        height: stopwatchMode ? 76 : 38
        radius: 7
        color: "#e6191d22"
        border.color: dragging ? "#43b6e8" : "#59616a"
        border.width: 1
        z: 1

        Text {
            id: display
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.top: parent.top
            anchors.topMargin: stopwatchMode ? 7 : 6
            text: stopwatchMode ? Stopwatch.formatElapsed(Stopwatch.elapsed(stopwatch, monotonicNow)) : clockText
            color: "#f2f4f5"
            font.pixelSize: stopwatchMode ? 22 : 21
            font.family: "sans-serif"
            font.weight: Font.DemiBold
        }

        Row {
            visible: stopwatchMode
            spacing: 8
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            anchors.bottomMargin: 7
            Repeater {
                model: [stopwatch.running ? "Pause" : "Play", "Reset"]
                delegate: Rectangle {
                    width: 92; height: 27; radius: 5; color: control.pressed ? "#52606b" : "#303840"
                    Text { anchors.centerIn: parent; text: modelData; color: "white"; font.pixelSize: 15 }
                    MouseArea {
                        id: control; anchors.fill: parent
                        onClicked: overlay.fetchMonotonic(function() {
                            if (index === 1) overlay.stopwatch = Stopwatch.reset()
                            else if (overlay.stopwatch.running) overlay.stopwatch = Stopwatch.pause(overlay.stopwatch, overlay.monotonicNow)
                            else overlay.stopwatch = Stopwatch.play(overlay.stopwatch, overlay.monotonicNow)
                        })
                    }
                }
            }
        }

        MouseArea {
            id: gesture
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            height: stopwatchMode ? 38 : parent.height
            pressAndHoldInterval: 600
            property real originMouseX
            property real originMouseY
            property real originPanelX
            property real originPanelY
            onPressed: { originMouseX = mouse.x; originMouseY = mouse.y; originPanelX = panel.x; originPanelY = panel.y; suppressClick = false }
            onPressAndHold: { dragging = true; suppressClick = true }
            onPositionChanged: if (dragging) {
                panel.x = overlay.clampX(originPanelX + mouse.x - originMouseX)
                panel.y = overlay.clampY(originPanelY + mouse.y - originMouseY)
            }
            onReleased: if (dragging) { dragging = false; overlay.positionSaveRequested(panel.x, panel.y) }
            onClicked: if (!suppressClick) stopwatchMode = !stopwatchMode
        }
    }
}
