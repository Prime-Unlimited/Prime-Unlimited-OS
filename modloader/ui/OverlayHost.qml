import QtQuick 2.12

Item {
    id: host
    anchors.fill: parent
    property var overlays: []
    property string snapshot: ""
    property string authToken: "__PRIME_UNLIMITED_AUTH_TOKEN__"
    property bool mainScreen: typeof planckQuickView === "undefined" ? true : planckQuickView.isMainScreen
    visible: mainScreen
    enabled: mainScreen

    function authorized(request) {
        request.setRequestHeader("Authorization", "Bearer " + authToken)
    }

    function loadPosition(id, item) {
        var request = new XMLHttpRequest()
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE || request.status !== 200) return
            try {
                var value = JSON.parse(request.responseText)
                if (item && item.restorePosition) item.restorePosition(value.x, value.y)
            } catch (e) {}
        }
        request.open("GET", "http://127.0.0.1:8098/api/v1/overlay-settings/" + id)
        authorized(request); request.send()
    }

    function savePosition(id, x, y) {
        var request = new XMLHttpRequest()
        request.open("POST", "http://127.0.0.1:8098/api/v1/overlay-settings/" + id)
        request.setRequestHeader("Content-Type", "application/json")
        authorized(request); request.send(JSON.stringify({"x": Math.round(x), "y": Math.round(y)}))
    }

    function refresh() {
        var request = new XMLHttpRequest()
        request.timeout = 1000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE || request.status !== 200) return
            if (request.responseText === snapshot) return
            try {
                var payload = JSON.parse(request.responseText)
                snapshot = request.responseText
                overlays = payload.overlays || []
            } catch (e) {
                overlays = []
            }
        }
        request.open("GET", "http://127.0.0.1:8098/api/v1/overlays")
        request.send()
    }

    Timer { interval: 1000; repeat: true; running: host.mainScreen; triggeredOnStart: true; onTriggered: host.refresh() }
    Repeater {
        model: host.mainScreen ? host.overlays : []
        delegate: Loader {
            anchors.fill: parent
            source: modelData.source
            onLoaded: {
                var overlayId = modelData.id
                if (item && item.positionSaveRequested) item.positionSaveRequested.connect(function(x, y) { host.savePosition(overlayId, x, y) })
                host.loadPosition(overlayId, item)
            }
            onStatusChanged: if (status === Loader.Error) console.warn("Prime Unlimited overlay failed:", modelData.id)
        }
    }
}
