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
    property bool updaterBusy: false
    property bool developerMode: false
    property var publishState: ({"status":"idle","modId":"","message":"","progress":0})
    property string operationModId: ""
    property int operationProgress: 0
    property string operationLabel: ""
    property bool developerPrompt: false
    property string developerStatus: "idle"
    property string developerUserCode: ""
    property string developerMessage: ""
    property bool showUpdatePrompt: false
    property var selfUpdate: ({"installed":{"version":"?","revision":0},"available":{"version":"?","revision":0},"updateAvailable":false,"rollbackAvailable":false})
    property int selectedTab: 0
    property var serviceAccess: null
    property string serviceAccessID: ""
    property int servicePairingSeconds: 0
    property string pendingUninstall: ""
    property string statusText: "Connecting..."
    property string deviceText: "Device not detected"
    signal closeRequested()

    function showServiceAccess(id) {
        serviceAccessID = id
        mutation("service-access", id, "", function(ok, payload) {
            if (ok) { serviceAccess = payload; servicePairingSeconds = payload.expiresIn || 120 }
        })
    }
    Timer { interval: 1000; repeat: true; running: serviceAccess !== null && servicePairingSeconds > 0
        onTriggered: servicePairingSeconds-- }

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
                developerMode = payload.developerMode === true
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
        refreshSelfUpdate()
        getJson("/api/v1/publish-status", function(payload) { publishState = payload }, true)
    }

    Timer {
        interval: 2000; repeat: true; running: publishState.status === "publishing"
        onTriggered: pollPublish()
    }

    function pollPublish() {
        getJson("/api/v1/publish-status", function(payload) {
            publishState = payload
            statusText = payload.message || "Publishing..."
            if (payload.status === "published") refresh()
        }, true)
    }

    function publishMod(id) {
        if (publishState.status === "publishing" || busy) return
        publishState = ({"status":"publishing","modId":id,"message":"Publishing...","progress":0})
        statusText = "Publishing..."
        mutation("publish", id, "", function(ok, payload) {
            if (ok) { publishState = payload; pollPublish() }
            else publishState = ({"status":"failed","modId":id,"message":payload.error || "Publishing failed","progress":0})
        })
    }

    function refreshSelfUpdate(done) {
        var request = new XMLHttpRequest(); request.timeout = 4000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            if (request.status === 200) {
                try {
                    selfUpdate = JSON.parse(request.responseText)
                    if (done) done(selfUpdate)
                }
                catch (e) { statusText = "Invalid self-updater response" }
            }
        }
        request.onerror = function() { if (updaterBusy) statusText = "Self-updater unavailable" }
        request.open("GET", "http://127.0.0.1:8099/api/v1/status"); request.send()
    }

    function restartModLoader() {
        var host = modLoader.parent
        if (!host) { modLoader.closeRequested(); return }
        var timer = Qt.createQmlObject('import QtQuick 2.12; Timer { interval: 3000; running: true; repeat: false }', host, "ModLoaderRestartTimer")
        timer.triggered.connect(function() {
            host.source = "file:///data/prime-unlimited/ui/ModLoader.qml?" + Date.now()
            host.active = true
            timer.destroy()
        })
        modLoader.closeRequested()
    }

    function updaterMutation(action, promptOnAvailable) {
        if (!authToken || updaterBusy) return
        var request = new XMLHttpRequest(); request.timeout = 720000; updaterBusy = true
        statusText = action === "check" ? "Checking for Modloader updates..." : "Updating Modloader..."
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            updaterBusy = false
            try {
                var payload = JSON.parse(request.responseText)
                var succeeded = request.status === 200
                statusText = succeeded
                    ? (action === "check" ? "Modloader update check completed" : "Modloader update completed")
                    : (payload.error || "Self-update failed")
                if (succeeded && action === "update") {
                    restartModLoader()
                    return
                }
            } catch(e) { statusText = "Invalid updater response" }
            refreshSelfUpdate(function(state) {
                if (action === "check" && promptOnAvailable && state.updateAvailable)
                    showUpdatePrompt = true
            })
        }
        request.onerror = function() {
            if (request.readyState === XMLHttpRequest.DONE && request.status >= 200) return
            updaterBusy = false
            var message = "Self-updater unavailable"
            try {
                var payload = JSON.parse(request.responseText)
                if (payload && payload.error) message = payload.error
            } catch (e) {}
            statusText = message
            refreshSelfUpdate()
        }
        request.ontimeout = function() { updaterBusy=false; statusText="Self-updater timed out" }
        request.open("POST", "http://127.0.0.1:8099/api/v1/" + action)
        request.setRequestHeader("Authorization", "Bearer " + authToken); request.send()
    }

    function selfUpdateButtonText() {
        if (updaterBusy || selfUpdate.checking) return selfUpdate.updateAvailable ? "UPDATING..." : "CHECKING..."
        return selfUpdate.updateAvailable ? "UPDATE" : "CHECK UPDATES"
    }

    function isApprovedForDevice(item) {
        if (!item.approval || !item.approval.maintainer || !item.approval.devices) return false
        var model = deviceText.split(" /")[0]
        for (var index = 0; index < item.approval.devices.length; ++index) {
            if (item.approval.devices[index] === model) return true
        }
        return false
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
        if (action === "safe-mode/enable" || action === "safe-mode/disable" || action === "catalog/sync") request.send()
        else request.send(JSON.stringify({"id": id}))
    }

    function installVerified(id) {
        var steps = ["cache", "stage", "inspect", "install"]
        operationModId = id; operationProgress = 0; operationLabel = "Download 0%"
        function next(index) {
            if (index >= steps.length) { operationProgress = 100; operationLabel = "Installation 100%"; statusText = "Verified installation completed"; refreshState(); return }
            operationProgress = index * 25
            operationLabel = index === 0 ? "Download " + operationProgress + "%" : "Installation " + operationProgress + "%"
            statusText = "Installation: " + steps[index]
            mutation(steps[index], id, "", function(ok) { if (ok) next(index + 1); else { operationModId = ""; operationProgress = 0 } })
        }
        next(0)
    }

    function updateVerified(id) {
        var wasEnabled = installed[id] && installed[id].enabled
        operationModId = id; operationProgress = 0; operationLabel = "Download 0%"
        function executeUpdate() {
            var steps = ["cache", "stage", "inspect", "update"]
            function next(index) {
                if (index >= steps.length) {
                    statusText = "Verified update completed"
                    if (wasEnabled) mutation("enable", id, "", function() { refresh() })
                    else refresh()
                    return
                }
                statusText = "Update: " + steps[index]
                operationProgress = index * 25
                operationLabel = index === 0 ? "Download " + operationProgress + "%" : "Installation " + operationProgress + "%"
                mutation(steps[index], id, "", function(ok) { if (ok) next(index + 1) })
            }
            next(0)
        }
        if (wasEnabled) mutation("disable", id, "", function(ok) { if (ok) executeUpdate() })
        else executeUpdate()
    }

    function syncCatalog() {
        statusText = "Syncing signed GitHub catalog..."
        mutation("catalog/sync", "", "", function() { refresh() })
    }

    function postDeveloperMode(enabled, done) {
        var request = new XMLHttpRequest()
        request.timeout = 15000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            var payload = ({})
            try { payload = JSON.parse(request.responseText) } catch (e) { payload = ({}) }
            if (request.status === 200) done(true, payload)
            else done(false, payload)
        }
        request.onerror = function() { done(false, ({"error": "Connection failed"})) }
        request.ontimeout = function() { done(false, ({"error": "Connection timed out"})) }
        request.open("POST", apiBase + "/api/v1/developer-mode")
        request.setRequestHeader("Content-Type", "application/json")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        request.send(JSON.stringify({"enabled": enabled}))
    }

    function applyDeveloperPayload(payload) {
        developerMode = payload.enabled === true
        developerStatus = payload.status || (developerMode ? "enabled" : "idle")
        developerUserCode = payload.userCode || ""
        developerMessage = payload.message || payload.error || ""
        if (developerMode) {
            developerPrompt = false
            refresh()
        }
    }

    function beginDeveloperLogin() {
        developerStatus = "pending"
        developerMessage = ""
        postDeveloperMode(true, function(ok, payload) {
            if (!ok) {
                developerStatus = "error"
                developerMessage = payload.error || "GitHub-Anmeldung fehlgeschlagen"
                return
            }
            applyDeveloperPayload(payload)
        })
    }

    function pollDeveloperLogin() {
        var request = new XMLHttpRequest()
        request.timeout = 4000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE || request.status !== 200) return
            try { applyDeveloperPayload(JSON.parse(request.responseText)) } catch (e) { return }
        }
        request.open("GET", apiBase + "/api/v1/developer-mode")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        request.send()
    }

    function openDeveloperPrompt() {
        developerPrompt = true
        if (developerMode) {
            developerStatus = "enabled"
            return
        }
        if (developerStatus === "pending" && developerUserCode !== "") return
        beginDeveloperLogin()
    }

    function disableDeveloperMode() {
        postDeveloperMode(false, function(ok, payload) {
            if (!ok) {
                developerMessage = payload.error || "Developer Mode blieb an"
                return
            }
            developerMode = false
            developerStatus = "idle"
            developerUserCode = ""
            developerPrompt = false
            refresh()
        })
    }

    Timer {
        interval: 2000
        repeat: true
        running: developerStatus === "pending"
        onTriggered: pollDeveloperLogin()
    }

    function rowLabel(modelData) {
        if (modelData.status === "development")
            return modelData.compatible ? "DEVELOPMENT" : "DEVELOPMENT â€” not compatible with this device"
        if (modelData.adminTestPreview) return "ADMIN TEST / awaiting signed catalog â€“ install unavailable"
        if (safety.mods && safety.mods[modelData.id] && safety.mods[modelData.id].quarantined)
            return "Quarantined after " + safety.mods[modelData.id].failureCount + " failures"
        if (modelData.updateAvailable) return "Update available: " + modelData.updateReason
        if (modelData.compatible) return "Official / compatible"
        if (modelData.deviceEligible && !isApprovedForDevice(modelData)) return "Developer preview / not approved"
        return "Not compatible with this device"
    }

    function visibleMods() {
        if (selectedTab === 0) {
            var available = []
            for (var index = 0; index < mods.length; ++index) {
                if (mods[index].status === "development" && !developerMode) continue
                var approved = isApprovedForDevice(mods[index])
                if ((approved && mods[index].compatible) || (developerMode && mods[index].deviceEligible) || mods[index].status === "development")
                    available.push(mods[index])
            }
            return available
        }
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

    Component.onCompleted: {
        refresh()
        updaterMutation("check", true)
    }

    Rectangle {
        id: header
        width: parent.width
        height: 64
        color: "#191c20"
        Text {
            id: headerTitle
            x: 10; y: 4; width: 370; height: 56
            verticalAlignment: Text.AlignVCenter
            text: "PRIME UNLIMITED MODLOADER"
            color: "#f4f2ee"; font.pixelSize: 30; font.bold: true; font.letterSpacing: -1.5
        }
        Row {
            id: headerControls
            anchors.right: parent.right
            anchors.rightMargin: 10
            y: 9; spacing: 4
            Repeater {
                model: ["AVAILABLE", "INSTALLED", "ACTIVITY"]
                delegate: Rectangle {
                    width: index === 1 ? 62 : 58; height: 46; radius: 9
                    color: selectedTab === index ? "#ff541c" : "#33373c"
                    Text { anchors.centerIn: parent; text: modelData; color: "white"; font.pixelSize: 9; font.bold: true }
                    MouseArea { anchors.fill: parent; onClicked: selectedTab = index }
                }
            }
            Rectangle {
                width: 70; height: 46; radius: 9
                color: developerMode ? "#d77921" : "#33373c"
                border.width: developerMode ? 2 : 1
                border.color: developerMode ? "#ffb15c" : "#555a60"
                Text {
                    anchors.centerIn: parent; color: "white"; font.pixelSize: 10; font.bold: true
                    text: developerMode ? "DEV MODE\nON" : "DEV MODE\nOFF"
                    horizontalAlignment: Text.AlignHCenter
                }
                MouseArea { anchors.fill: parent; onClicked: openDeveloperPrompt() }
            }
            Rectangle {
                width: 82; height: 46; radius: 9
                color: selfUpdate.updateAvailable ? "#d77921" : "#34516b"
                Text {
                    anchors.centerIn: parent; width: parent.width - 6
                    horizontalAlignment: Text.AlignHCenter
                    color: "white"; font.pixelSize: 9; font.bold: true; text: selfUpdateButtonText()
                }
                MouseArea {
                    anchors.fill: parent; enabled: !updaterBusy
                    onClicked: updaterMutation(selfUpdate.updateAvailable ? "update" : "check", true)
                }
            }
            Rectangle {
                width: 52; height: 46; radius: 9; color: "#33373c"
                Text { anchors.centerIn: parent; text: "CLOSE"; color: "white"; font.pixelSize: 11; font.bold: true }
                MouseArea { anchors.fill: parent; onClicked: modLoader.closeRequested() }
            }
        }
    }
    Text {
        id: device
        x: 20; y: 77
        text: deviceText + "   MODLOADER v" + selfUpdate.installed.version + "-r" + selfUpdate.installed.revision
        color: "#aab0b8"
        font.pixelSize: 17
    }
    Rectangle {
        x: 440; y: 108; width: 340; height: 8; radius: 4
        visible: updaterBusy
        color: "#33373c"
        Rectangle { height: parent.height; radius: 4; color: "#ff541c"; width: parent.width * Math.max(0, Math.min(100, Number(selfUpdate.progress || 0))) / 100 }
    }
    Text {
        id: status
        x: 20; y: 105
        width: 410
        text: statusText
        color: "#ff541c"
        font.pixelSize: 14
        wrapMode: Text.WordWrap
        maximumLineCount: 2
    }
    Row {
        anchors.right: parent.right
        anchors.rightMargin: 20
        y: 76; spacing: 10
        Rectangle {
            width: 126; height: 42; radius: 9; color: "#34516b"
            Text { anchors.centerIn: parent; color: "white"; font.pixelSize: 12; text: "SYNC CATALOG" }
            MouseArea { anchors.fill: parent; enabled: !busy; onClicked: syncCatalog() }
        }
        Rectangle {
            width: 180; height: 42; radius: 9
            color: safety.safeMode ? "#8d2d21" : "#264536"
            Text { anchors.centerIn: parent; color: "white"; font.pixelSize: 13; text: safety.safeMode ? "SAFE MODE ON" : "SAFE MODE OFF" }
            MouseArea { anchors.fill: parent; enabled: !busy; onClicked: mutation(safety.safeMode ? "safe-mode/disable" : "safe-mode/enable", "", "", null) }
        }
    }
    Flickable {
        visible: selectedTab !== 2
        x: 20; y: 132
        width: parent.width - 40
        height: parent.height - 148
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
                    height: 148
                    radius: 12
                    color: "#22262b"
                    Text {
                        x: 16; y: 12
                        text: modelData.name + "  v" + modelData.version + "  r" + (modelData.revision || 0)
                        color: "#f4f2ee"
                        font.pixelSize: 21
                        font.bold: true
                    }
                    Text {
                        x: 16; y: 48
                        text: publishState.status === "publishing" && publishState.modId === modelData.id
                            ? "Publishing... " + (publishState.progress || 0) + "%"
                            : (operationModId === modelData.id ? operationLabel : rowLabel(modelData))
                        color: safety.mods && safety.mods[modelData.id] && safety.mods[modelData.id].quarantined ? "#ff9b78" : (modelData.compatible ? "#73d6a3" : "#ff9b78")
                        font.pixelSize: 16
                    }
                    Text {
                        x: 330; y: 13; width: 250
                        text: (modelData.dependencies || []).length + " mod dependencies, " + (modelData.libraries || []).length + " libraries"
                        color: "#aab0b8"; font.pixelSize: 13; elide: Text.ElideRight
                    }
                    Text {
                        x: 16; y: 75; width: 300
                        visible: installed[modelData.id] !== undefined
                        text: "Installed: v" + installed[modelData.id].version + " r" + (installed[modelData.id].revision || 0) + "  SHA " + installed[modelData.id].packageSha256.substring(0, 10) + "..."
                        color: modelData.updateAvailable ? "#ffb15c" : "#aab0b8"; font.pixelSize: 12; elide: Text.ElideRight
                    }
                    Rectangle {
                        visible: installed[modelData.id] !== undefined && modelData.updateAvailable
                        width: 104; height: 48; radius: 9; x: parent.width - 334; y: 19; color: "#d77921"
                        Text { anchors.centerIn: parent; text: "UPDATE"; color: "white"; font.bold: true }
                        MouseArea { anchors.fill: parent; enabled: !busy && modelData.compatible; onClicked: updateVerified(modelData.id) }
                    }
                    Rectangle {
                        visible: developerMode && modelData.status === "development"
                        width: 104; height: 48; radius: 9; x: parent.width - 444; y: 19
                        color: publishState.status === "publishing" && publishState.modId === modelData.id ? "#6b512e" : "#d77921"
                        Text { anchors.centerIn: parent; color: "white"; font.pixelSize: 10; font.bold: true
                            text: publishState.status === "publishing" && publishState.modId === modelData.id ? "PUBLISHING..." : "PUBLISH" }
                        MouseArea { anchors.fill: parent; enabled: publishState.status !== "publishing" && !busy; onClicked: publishMod(modelData.id) }
                    }
                    Rectangle {
                        width: 104; height: 48; radius: 9; x: parent.width - (present ? 224 : 116); y: 19
                        color: present && installed[modelData.id].enabled ? "#1f8a4c" : "#ff541c"
                        property bool present: installed[modelData.id] !== undefined
                        Text { anchors.centerIn: parent; color: "white"; font.bold: true
                            text: modelData.adminTestPreview ? "PENDING" : (parent.present ? (installed[modelData.id].enabled ? "DISABLE" : "ENABLE") : "INSTALL") }
                        MouseArea { anchors.fill: parent; enabled: !busy && !modelData.adminTestPreview && (parent.present || modelData.compatible); onClicked: {
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
                    Rectangle {
                        visible: modelData.service !== undefined && installed[modelData.id] !== undefined
                        x: 445; y: 91; width: 110; height: 28; radius: 7; color: "#ff541c"
                        Text { anchors.centerIn: parent; text: "WEB ACCESS"; color: "white"; font.bold: true }
                        MouseArea { anchors.fill: parent; enabled: !busy && installed[modelData.id] && installed[modelData.id].enabled
                            onClicked: showServiceAccess(modelData.id) }
                    }
                    Row {
                        visible: installed[modelData.id] !== undefined
                        x: 330; y: 91; spacing: 8
                        Rectangle {
                            width: 104; height: 28; radius: 7; color: "#5a456b"
                            Text { anchors.centerIn: parent; text: "ROLLBACK"; color: "white"; font.bold: true }
                            MouseArea { anchors.fill: parent; enabled: installed[modelData.id] && !installed[modelData.id].enabled && !busy; onClicked: mutation("rollback", modelData.id, "", function() { refresh() }) }
                        }
                    }
                    Rectangle {
                        visible: operationModId === modelData.id || (publishState.status === "publishing" && publishState.modId === modelData.id)
                        x: 16; y: 132; width: parent.width - 32; height: 7; radius: 3; color: "#33373c"
                        Rectangle {
                            height: parent.height; radius: 3; color: "#ff541c"
                            width: parent.width * Math.max(0, Math.min(100, publishState.status === "publishing" && publishState.modId === modelData.id ? publishState.progress : operationProgress)) / 100
                        }
                    }
                }
            }
        }
    }
    Rectangle {
        visible: serviceAccess !== null
        anchors.fill: parent; color: "#ed101215"; z: 30
        Rectangle {
            anchors.centerIn: parent; width: 720; height: 395; radius: 14; color: "#22262b"
            Text { x: 24; y: 20; text: "SERVICE WEB ACCESS"; color: "#ff541c"; font.pixelSize: 22; font.bold: true }
            Text { x: 24; y: 58; width: 672; color: "white"; font.pixelSize: 17; wrapMode: Text.WrapAnywhere
                text: serviceAccess ? "Open on your phone or computer:\n" + (serviceAccess.urls || []).join("\n") : "" }
            Text { x: 24; y: 140; width: 672; color: "white"; font.pixelSize: 28; wrapMode: Text.WrapAnywhere
                text: serviceAccess ? (servicePairingSeconds > 0 ? "Pairing code: " + serviceAccess.pairingCode + "\nValid for " + servicePairingSeconds + " s" : "Code expired. Tap NEW CODE.") : "" }
            Text { x: 24; y: 220; width: 672; color: "#bfc3c9"; font.pixelSize: 14; wrapMode: Text.WrapAnywhere
                text: serviceAccess ? "Device certificate is self-signed. Compare its SHA-256 fingerprint before accepting it:\n" + serviceAccess.certificateSHA256 : "" }
            Rectangle { x: 160; y: 325; width: 180; height: 48; radius: 8; color: "#ff541c"
                Text { anchors.centerIn: parent; text: "NEW CODE"; color: "white" }
                MouseArea { anchors.fill: parent; enabled: !busy; onClicked: showServiceAccess(serviceAccessID) }
            }
            Rectangle { x: 380; y: 325; width: 180; height: 48; radius: 8; color: "#33373c"
                Text { anchors.centerIn: parent; text: "CLOSE"; color: "white" }
                MouseArea { anchors.fill: parent; onClicked: serviceAccess = null }
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
    Rectangle {
        visible: showUpdatePrompt
        anchors.fill: parent; color: "#d9101215"; z: 20
        Rectangle {
            width: 520; height: 210; radius: 14; anchors.centerIn: parent; color: "#22262b"
            Text {
                x: 24; y: 24; width: 472; wrapMode: Text.WordWrap
                color: "white"; font.pixelSize: 20; font.bold: true
                text: "MODLOADER UPDATE AVAILABLE\n\nv" + selfUpdate.available.version + "-r" + selfUpdate.available.revision + " installieren?"
            }
            Row {
                anchors.horizontalCenter: parent.horizontalCenter; y: 136; spacing: 18
                Rectangle {
                    width: 170; height: 52; radius: 10; color: "#33373c"
                    Text { anchors.centerIn: parent; text: "NO"; color: "white"; font.bold: true }
                    MouseArea { anchors.fill: parent; onClicked: showUpdatePrompt = false }
                }
                Rectangle {
                    width: 170; height: 52; radius: 10; color: "#ff541c"
                    Text { anchors.centerIn: parent; text: "YES"; color: "white"; font.bold: true }
                    MouseArea { anchors.fill: parent; onClicked: { showUpdatePrompt = false; updaterMutation("update", false) } }
                }
            }
        }
    }
    Rectangle {
        visible: developerPrompt
        anchors.fill: parent; color: "#d9101215"; z: 25
        Rectangle {
            width: 640; height: developerMode ? 220 : 430; radius: 14; anchors.centerIn: parent; color: "#22262b"
            Text {
                x: 24; y: 16; width: 592; wrapMode: Text.WordWrap
                color: "white"; font.pixelSize: 20; font.bold: true
                text: developerMode ? "Developer Mode ausschalten" : "Developer Mode einschalten"
            }
            Image {
                x: 24; y: 58; width: 168; height: 168
                visible: !developerMode
                source: apiBase + "/api/v1/developer-mode/qr.png"
                fillMode: Image.PreserveAspectFit
            }
            Text {
                x: 210; y: 64; width: 400; visible: !developerMode
                color: "white"; font.pixelSize: 32; font.bold: true
                text: developerUserCode === "" ? "â€¦" : developerUserCode
            }
            Text {
                x: 210; y: 116; width: 400; visible: !developerMode
                color: "#bfc3c9"; font.pixelSize: 15; wrapMode: Text.WordWrap
                text: "QR scannen, bei GitHub anmelden und diesen Code eingeben. Nur Admins von Prime-Unlimited/Prime-Unlimited-OS."
            }
            Text {
                x: 210; y: 190; width: 400; visible: !developerMode
                color: "#8fd0ff"; font.pixelSize: 14
                text: "https://github.com/login/device"
            }
            Text {
                x: 24; y: developerMode ? 64 : 246; width: 592
                color: developerStatus === "error" || developerStatus === "denied" || developerStatus === "expired" ? "#ff9b78" : "#bfc3c9"
                font.pixelSize: 15; wrapMode: Text.WordWrap
                text: developerMode ? "Development-Mods verschwinden aus der Liste. Einschalten geht wieder Ã¼ber GitHub." : (developerMessage !== "" ? developerMessage : "Warte auf die Anmeldung am Handy. Der Code gilt 15 Minuten.")
            }
            Row {
                anchors.horizontalCenter: parent.horizontalCenter; y: developerMode ? 140 : 360; spacing: 16
                Rectangle {
                    width: 150; height: 46; radius: 10; color: "#33373c"
                    Text { anchors.centerIn: parent; text: "CANCEL"; color: "white"; font.bold: true }
                    MouseArea { anchors.fill: parent; onClicked: { if (developerMode) developerPrompt = false; else disableDeveloperMode() } }
                }
                Rectangle {
                    visible: developerMode
                    width: 180; height: 46; radius: 10; color: "#d77921"
                    Text { anchors.centerIn: parent; text: "AUSSCHALTEN"; color: "white"; font.bold: true }
                    MouseArea { anchors.fill: parent; onClicked: disableDeveloperMode() }
                }
            }
        }
    }
    Flickable {
        visible: selectedTab === 2
        x: 20; y: 132; width: parent.width - 40; height: parent.height - 148
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

