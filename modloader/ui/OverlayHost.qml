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

    property string nativeProbeRevision: "0"
    property bool nativeProbeEnabled: false
    property bool nativeProbeRefreshing: false
    function refreshNativeProbe() {
        if (nativeProbeRefreshing) return
        nativeProbeRefreshing = true
        var request = new XMLHttpRequest()
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            nativeProbeRefreshing = false
            if (request.status !== 200) { nativeProbeEnabled = false; return }
            try { var payload = JSON.parse(request.responseText); var revision = String(payload.scriptRevision || "0"); if (/^[0-9]{1,20}$/.test(revision)) nativeProbeRevision = revision; nativeProbeEnabled = payload.enabled === true } catch (e) { nativeProbeEnabled = false }
        }
        request.open("GET", "http://127.0.0.1:8098/api/v1/native-playlist/probe")
        host.authorized(request)
        request.send()
    }
    Timer { interval: 3000; repeat: true; running: host.mainScreen; onTriggered: host.refreshNativeProbe() }
    Loader {
        source: host.nativeProbeEnabled ? "file:///data/prime-unlimited/ui/NativePlaylistProbe.qml?probe=" + host.nativeProbeRevision : ""
        onLoaded: { item.hostItem = host; item.authToken = host.authToken }
    }

    QtObject {
        id: mediaImportBridge
        property var events: []
        property var jobs: []
        property bool jobsRefreshing: false
        property bool available: false
        property bool preparing: false
        property int generation: 0

        function refresh() {
            if (preparing) return
            var epoch = generation
            var request = new XMLHttpRequest()
            request.timeout = 1000
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                if (preparing || epoch !== generation) return
                if (request.status !== 200) {
                    mediaImportBridge.available = false
                    mediaImportBridge.events = []
                    return
                }
                try {
                    var payload = JSON.parse(request.responseText)
                    mediaImportBridge.events = payload.events || []
                    mediaImportBridge.available = true
                } catch (e) {
                    mediaImportBridge.available = false
                    mediaImportBridge.events = []
                }
            }
            request.open("GET", "http://127.0.0.1:8098/api/v1/media-import/events")
            host.authorized(request)
            request.send()
        }

        property string decisionEventId: ""
        property string preparationMessage: "Musik wird geprüft …"
        property bool progressRefreshing: false
        property var decisionRequest: null
        property var decisionCallback: null

        function finishDecision(request, status, responseText) {
            if (decisionRequest !== request) return
            decisionWatchdog.stop()
            var callback = decisionCallback
            decisionRequest = null
            decisionCallback = null
            preparing = false
            if (callback) callback(status >= 200 && status < 300, status, responseText)
        }

        function respond(eventId, decision, destinationId, callback) {
            if (preparing) { if (callback) callback(false, 409, ""); return }
            preparing = true
            generation++
            var request = new XMLHttpRequest()
            decisionRequest = request
            decisionEventId = eventId
            preparationMessage = "USB-Stick wird eingelesen …"
            decisionCallback = callback
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                mediaImportBridge.finishDecision(request, request.status, request.responseText)
            }
            request.open("POST", "http://127.0.0.1:8098/api/v1/media-import/events")
            request.setRequestHeader("Content-Type", "application/json")
            host.authorized(request)
            decisionWatchdog.restart()
            request.send(JSON.stringify({"eventId": eventId, "decision": decision, "destinationId": destinationId || ""}))
        }

        function refreshPreparation() {
            if (!decisionRequest || progressRefreshing) return
            progressRefreshing = true
            var expectedRequest = decisionRequest
            var request = new XMLHttpRequest()
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                progressRefreshing = false
                if (decisionRequest !== expectedRequest || request.status !== 200) return
                try {
                    var progress = JSON.parse(request.responseText)
                    if (progress.eventId !== decisionEventId) return
                    var labels = {scan: "USB-Stick wird eingelesen", plan: "Zielordner wird vorbereitet", source: "USB-Dateien werden geprüft", index: "Interne Bibliothek wird geprüft", compare: "Doppelte Dateien werden verglichen"}
                    preparationMessage = (labels[progress.stage] || "Vorschau wird vorbereitet") + (progress.total > 0 ? " · " + progress.done + " / " + progress.total : " …")
                } catch (e) {}
            }
            request.open("GET", "http://127.0.0.1:8098/api/v1/media-import/preparation")
            host.authorized(request)
            request.send()
        }

        function startImport(eventId, callback, mode) {
            if (preparing) { if (callback) callback(false, 409, ""); return }
            preparing = true
            generation++
            var request = new XMLHttpRequest()
            request.timeout = 15000
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                mediaImportBridge.preparing = false
                if (callback) callback(request.status >= 200 && request.status < 300, request.status, request.responseText)
                mediaImportBridge.refreshJobs()
            }
            request.open("POST", "http://127.0.0.1:8098/api/v1/media-import/start")
            request.setRequestHeader("Content-Type", "application/json")
            host.authorized(request)
            var input = {eventId: eventId}
            if (mode === "live") { input.mode = "live"; input.confirmCopy = true }
            else { if (mode === "offline") input.mode = "offline"; input.confirmEngineRestart = true }
            request.send(JSON.stringify(input))
        }
        function refreshJobs() {
            if (preparing || jobsRefreshing) return
            jobsRefreshing = true
            var epoch = generation
            var request = new XMLHttpRequest()
            request.timeout = 3000
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                mediaImportBridge.jobsRefreshing = false
                if (epoch !== generation || preparing || request.status !== 200) return
                try { mediaImportBridge.jobs = JSON.parse(request.responseText).jobs || [] } catch (e) {}
            }
            request.open("GET", "http://127.0.0.1:8098/api/v1/media-import/jobs")
            host.authorized(request)
            request.send()
        }
        function acknowledgeJob(jobId, callback) {
            if (preparing) { if (callback) callback(false, 409, ""); return }
            preparing = true
            generation++
            var request = new XMLHttpRequest()
            request.timeout = 10000
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                mediaImportBridge.preparing = false
                mediaImportBridge.generation++
                if (callback) callback(request.status >= 200 && request.status < 300, request.status, request.responseText)
                mediaImportBridge.refreshJobs()
            }
            request.open("POST", "http://127.0.0.1:8098/api/v1/media-import/jobs/acknowledge")
            request.setRequestHeader("Content-Type", "application/json")
            host.authorized(request)
            request.send(JSON.stringify({jobId: jobId}))
        }

        function loadPreview(eventId, callback) {
            var request = new XMLHttpRequest()
            request.timeout = 3000
            request.onreadystatechange = function() {
                if (request.readyState === XMLHttpRequest.DONE && callback)
                    callback(request.status >= 200 && request.status < 300, request.status, request.responseText)
            }
            request.open("GET", "http://127.0.0.1:8098/api/v1/media-import/preview")
            request.setRequestHeader("X-USB-Copy-Event", eventId)
            host.authorized(request)
            request.send()
        }

        function discardPreview(eventId, callback) {
            if (preparing) { if (callback) callback(false, 409, ""); return }
            preparing = true
            generation++
            var request = new XMLHttpRequest()
            request.timeout = 10000
            request.onreadystatechange = function() {
                if (request.readyState !== XMLHttpRequest.DONE) return
                mediaImportBridge.preparing = false
                if (callback) callback(request.status >= 200 && request.status < 300, request.status, request.responseText)
            }
            request.open("DELETE", "http://127.0.0.1:8098/api/v1/media-import/preview")
            request.setRequestHeader("Content-Type", "application/json")
            host.authorized(request)
            request.send(JSON.stringify({"eventId": eventId}))
        }
    }

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

    function refreshMediaImport() {
        var usbCopyEnabled = false
        for (var i = 0; i < overlays.length; i++) {
            if (overlays[i].id === "usb-copy") usbCopyEnabled = true
        }
        if (usbCopyEnabled) { mediaImportBridge.refresh(); mediaImportBridge.refreshJobs() }
        else {
            mediaImportBridge.events = []
            mediaImportBridge.jobs = []
            mediaImportBridge.available = false
        }
    }

    function refresh() {
        var request = new XMLHttpRequest()
        request.timeout = 1000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE || request.status !== 200) return
            if (request.responseText === snapshot) {
                host.refreshMediaImport()
                return
            }
            try {
                var payload = JSON.parse(request.responseText)
                snapshot = request.responseText
                overlays = payload.overlays || []
                host.refreshMediaImport()
            } catch (e) {
                overlays = []
                host.refreshMediaImport()
            }
        }
        request.open("GET", "http://127.0.0.1:8098/api/v1/overlays")
        request.send()
    }

    Timer {
        interval: 500
        repeat: true
        running: mediaImportBridge.decisionRequest !== null
        onTriggered: mediaImportBridge.refreshPreparation()
    }

    Timer {
        id: decisionWatchdog
        interval: 180000
        repeat: false
        onTriggered: {
            var request = mediaImportBridge.decisionRequest
            if (!request) return
            mediaImportBridge.finishDecision(request, 0, "")
            request.abort()
        }
    }

    Timer { interval: 1000; repeat: true; running: host.mainScreen; triggeredOnStart: true; onTriggered: host.refresh() }
    Repeater {
        model: host.mainScreen ? host.overlays : []
        delegate: Loader {
            anchors.fill: parent
            source: modelData.source
            onLoaded: {
                var overlayId = modelData.id
                if (overlayId === "usb-copy" && item) item.mediaImportBridge = mediaImportBridge
                if (item && item.positionSaveRequested) item.positionSaveRequested.connect(function(x, y) { host.savePosition(overlayId, x, y) })
                host.loadPosition(overlayId, item)
            }
            onStatusChanged: if (status === Loader.Error) console.warn("Prime Unlimited overlay failed:", modelData.id)
        }
    }
}
