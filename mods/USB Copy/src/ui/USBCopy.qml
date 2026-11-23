import QtQuick 2.12

Item {
    id: overlay
    anchors.fill: parent
    property var mediaImportBridge: null
    property var currentEvent: null
    property bool choosingDestination: false
    property bool busy: false
    property string message: ""
    property var preview: null
    property string previewEventId: ""
    property var currentJob: null
    property bool confirmRestart: false

    function syncEvent() {
        if (currentJob) return
        if (!mediaImportBridge || !mediaImportBridge.available) {
            currentEvent = null
            choosingDestination = false
            preview = null
            previewEventId = ""
            return
        }
        var events = mediaImportBridge.events || []
        if (currentEvent) {
            for (var i = 0; i < events.length; i++) {
                if (events[i].id === currentEvent.id) {
                    currentEvent = events[i]
                    return
                }
            }
        }
        currentEvent = events.length ? events[0] : null
        choosingDestination = false
        message = ""
        preview = null
    }

    function respond(decision, destinationId) {
        if (!currentEvent || !mediaImportBridge || busy) return
        var eventId = currentEvent.id
        busy = true
        message = decision === "copy" ? "Musik und vorhandene Dateien werden geprüft. Das kann einige Minuten dauern …" : ""
        mediaImportBridge.respond(eventId, decision, destinationId, function(accepted, status, responseText) {
            busy = false
            if (!currentEvent || currentEvent.id !== eventId) return
            if (!accepted) {
                message = status === 0 ? "Keine Antwort erhalten. Bitte den Stick sicher auswerfen und erneut einstecken." : "Import konnte nicht vorbereitet werden (HTTP " + status + ")."
                return
            }
            if (decision === "copy") {
                var result = null
                try { result = JSON.parse(responseText) } catch (e) {}
                if (!result || !result.prepared) {
                    message = "Importvorschau konnte nicht gelesen werden."
                    return
                }
                message = ""
                previewEventId = eventId
                preview = result
                choosingDestination = false
            } else {
                currentEvent = null
                choosingDestination = false
                mediaImportBridge.refresh()
            }
        })
    }

    function closePreview() {
        if (!preview || !mediaImportBridge || busy) return
        busy = true
        mediaImportBridge.discardPreview(previewEventId, function(accepted, status, responseText) {
            busy = false
            if (!accepted && status !== 404 && status !== 410) {
                message = "Vorschau konnte nicht geschlossen werden (HTTP " + status + ")."
                return
            }
            preview = null
            previewEventId = ""
            confirmRestart = false
            currentEvent = null
            choosingDestination = false
            message = ""
            mediaImportBridge.refresh()
        })
    }

    function terminalJob() {
        return currentJob && ["completed", "failed", "cancelled", "interrupted"].indexOf(currentJob.phase) >= 0
    }
    function syncJobs() {
        var jobs = mediaImportBridge ? (mediaImportBridge.jobs || []) : []
        var next = null
        for (var i = 0; i < jobs.length; i++) if (!jobs[i].acknowledged) { next = jobs[i]; break }
        currentJob = next
        if (next) { currentEvent = null; preview = null; choosingDestination = false; confirmRestart = false }
        else syncEvent()
    }
    function jobMessage() {
        if (!currentJob) return ""
        var phases = {queued: "Import wird vorbereitet …", stopping_engine: "Engine wird angehalten …", copying: "Musik wird kopiert …", committing_library: "Bibliothek und Playlists werden aktualisiert …", restarting_engine: "Engine wird gestartet …", completed: "Import abgeschlossen", failed: "Import fehlgeschlagen", cancelled: "Import abgebrochen", interrupted: "Import unterbrochen. Bitte vor einem erneuten Import prüfen."}
        if (currentJob.phase === "completed" && currentJob.resultAvailable) {
            var copied = currentJob.copiedFiles || 0
            var skipped = currentJob.skippedFiles || 0
            var count = currentJob.createdPlaylists || 0
            return "Import abgeschlossen\n" + copied + " neu kopiert · " + skipped + " übersprungen\n" + (count > 0 ? count + (count === 1 ? " Playlist erstellt" : " Playlists erstellt") + "\n\n" + currentJob.playlistName : "Keine neue Playlist erstellt.")
        }
        var reason = ""
        if (currentJob.phase === "failed") {
            var reasons = {invalid_mp3: "Mindestens eine MP3-Datei konnte nicht gelesen werden.", source_scan_failed: "Der USB-Stick konnte nicht vollständig gelesen werden.", unsupported_audio: "Der Import unterstützt derzeit nur MP3-Dateien.", unsupported_library: "Die Version der Zielbibliothek wird nicht unterstützt.", library_integrity_failed: "Die Zielbibliothek hat die Integritätsprüfung nicht bestanden.", source_changed: "Die Dateien auf dem Stick wurden nach der Vorschau verändert.", volumes_changed: "Quelle oder interner Datenträger ist nicht mehr verfügbar.", engine_stop_failed: "Engine konnte nicht sicher angehalten werden.", recovery_unavailable: "Die automatische Wiederherstellung konnte nicht vorbereitet werden.", import_timeout: "Die maximale Importdauer wurde überschritten.", import_cancelled: "Der Import wurde abgebrochen.", worker_setup_failed: "Der Import konnte nicht vorbereitet werden.", start_failed: "Der Importprozess konnte nicht gestartet werden."}
            reason = "\n" + (reasons[currentJob.errorCode] || "Der Import wurde abgebrochen. Bitte den Fehler prüfen lassen.")
        }
        return (phases[currentJob.phase] || "Importstatus unbekannt") + "\n" + currentJob.filesDone + " / " + currentJob.filesTotal + " Titel verarbeitet" + reason
    }
    function startImport() {
        if (!preview || !preview.commitAvailable || !confirmRestart || busy) return
        busy = true
        mediaImportBridge.startImport(previewEventId, function(accepted, status, responseText) {
            busy = false
            if (!accepted) { message = "Importstart konnte nicht bestätigt werden (HTTP " + status + ")."; mediaImportBridge.refreshJobs(); return }
            try { currentJob = JSON.parse(responseText).job || null } catch (e) { currentJob = null }
            if (currentJob) { currentEvent = null; preview = null; choosingDestination = false; confirmRestart = false; message = "" }
        }, preview.mode || "offline")
    }
    function closeJob() {
        if (!terminalJob() || busy) return
        busy = true
        mediaImportBridge.acknowledgeJob(currentJob.id, function(accepted, status, responseText) {
            busy = false
            if (!accepted) { message = "Ergebnis konnte nicht bestätigt werden (HTTP " + status + ")."; return }
            currentJob = null
            message = ""
            mediaImportBridge.refreshJobs()
        })
    }

    Connections {
        target: overlay.mediaImportBridge
        onEventsChanged: overlay.syncEvent()
        onJobsChanged: overlay.syncJobs()
        onAvailableChanged: overlay.syncEvent()
    }

    Rectangle {
        anchors.fill: parent
        visible: overlay.currentEvent !== null || overlay.preview !== null || overlay.currentJob !== null
        color: "#99000000"
        z: 1000

        MouseArea { anchors.fill: parent }

        Rectangle {
            id: panel
            width: Math.min(parent.width - 48, 560)
            height: (overlay.preview || overlay.currentJob) ? Math.min(parent.height - 48, 430) : (overlay.choosingDestination ? Math.min(parent.height - 48, 430) : 284)
            anchors.centerIn: parent
            radius: 10
            color: "#20262d"
            border.color: "#58616a"
            border.width: 1

            Column {
                anchors.fill: parent
                anchors.margins: 24
                spacing: 16

                Text {
                    width: parent.width
                    text: overlay.currentJob ? "USB Copy" : (overlay.preview ? "Importvorschau" : (overlay.choosingDestination ? "Internen Datenträger wählen" : "Datenträger erkannt"))
                    color: "#f4f6f8"
                    font.pixelSize: 25
                    font.weight: Font.DemiBold
                    horizontalAlignment: Text.AlignHCenter
                }

                Text {
                    width: parent.width
                    visible: overlay.currentJob !== null
                    text: overlay.jobMessage()
                    color: "#d9dee3"
                    font.pixelSize: 18
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }

                Text {
                    width: parent.width
                    visible: overlay.preview !== null
                    text: overlay.preview ? (overlay.preview.trackCount + " Quelldateien · " + overlay.preview.playlistCount + " Playlists\n" + overlay.preview.newFiles + " neu · " + overlay.preview.existingFiles + " vorhanden · " + overlay.preview.sourceDuplicates + " doppelt auf Quelle\n" + (overlay.preview.bytesToCopy / 1073741824).toFixed(2) + " GB zu kopieren") : ""
                    color: "#d9dee3"
                    font.pixelSize: 18
                    horizontalAlignment: Text.AlignHCenter
                }

                Text {
                    width: parent.width
                    visible: !overlay.currentJob && !overlay.choosingDestination && !overlay.preview
                    text: "Möchtest du die Musik von „" + (overlay.currentEvent ? overlay.currentEvent.displayName : "USB") + "“ auf einen internen Datenträger kopieren?"
                    color: "#d9dee3"
                    font.pixelSize: 18
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }

                Text {
                    width: parent.width
                    visible: !overlay.preview && overlay.choosingDestination && (!overlay.currentEvent || !overlay.currentEvent.destinations || overlay.currentEvent.destinations.length === 0)
                    text: "Kein geeigneter interner Datenträger ist verfügbar."
                    color: "#d9dee3"
                    font.pixelSize: 17
                    horizontalAlignment: Text.AlignHCenter
                }

                ListView {
                    id: destinationList
                    width: parent.width
                    height: Math.max(0, parent.height - 124)
                    visible: !overlay.busy && !overlay.preview && overlay.choosingDestination && overlay.currentEvent && overlay.currentEvent.destinations && overlay.currentEvent.destinations.length > 0
                    clip: true
                    model: visible ? overlay.currentEvent.destinations : []
                    spacing: 8

                    delegate: Rectangle {
                        width: destinationList.width
                        height: 64
                        radius: 6
                        color: destinationMouse.pressed ? "#3d4852" : "#303840"
                        border.color: "#59636d"

                        Column {
                            anchors.left: parent.left
                            anchors.leftMargin: 14
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 3
                            Text { text: modelData.name; color: "#f4f6f8"; font.pixelSize: 17; font.weight: Font.DemiBold }
                            Text {
                                text: Math.floor(modelData.freeBytes / 1073741824) + " GB frei"
                                color: "#b9c1c8"
                                font.pixelSize: 14
                            }
                        }
                        MouseArea {
                            id: destinationMouse
                            anchors.fill: parent
                            enabled: !overlay.busy
                            onClicked: overlay.respond("copy", modelData.id)
                        }
                    }
                }

                Text {
                    width: parent.width
                    visible: overlay.message !== ""
                    text: overlay.busy && overlay.mediaImportBridge ? overlay.mediaImportBridge.preparationMessage : overlay.message
                    color: "#ffd27a"
                    font.pixelSize: 15
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }

                Text {
                    width: parent.width
                    visible: overlay.preview !== null
                    text: overlay.confirmRestart ? (overlay.preview && overlay.preview.mode === "live" ? "Musik und Playlists werden ohne Engine-Neustart importiert." : "Engine wird neu gestartet. Eine laufende Wiedergabe wird beendet.") : "Vorschau bereit. Es wurde noch nichts kopiert."
                    color: "#ffd27a"
                    font.pixelSize: 14
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }

                Row {
                    width: parent.width
                    spacing: 12
                    layoutDirection: Qt.RightToLeft

                    Rectangle {
                        width: overlay.choosingDestination ? 150 : 132
                        height: 44
                        visible: overlay.currentJob !== null || overlay.preview !== null || !overlay.choosingDestination || !overlay.currentEvent || !overlay.currentEvent.destinations || overlay.currentEvent.destinations.length === 0
                        radius: 6
                        color: !copyMouse.enabled ? "#434a50" : (copyMouse.pressed ? "#1685b5" : "#0878a8")
                        Text {
                            anchors.centerIn: parent
                            text: overlay.currentJob ? (overlay.terminalJob() ? "Schließen" : "Import läuft …") : (overlay.preview ? (overlay.preview.commitAvailable ? (overlay.confirmRestart ? "Jetzt starten" : "Kopieren") : "Schließen") : (overlay.busy ? "Bitte warten …" : (overlay.choosingDestination ? "Zurück" : "Ja, kopieren")))
                            color: copyMouse.enabled ? "white" : "#a0a7ad"
                            font.pixelSize: 16
                            font.weight: Font.DemiBold
                        }
                        MouseArea {
                            id: copyMouse
                            anchors.fill: parent
                            enabled: !overlay.busy && (overlay.currentJob ? overlay.terminalJob() : (overlay.preview || overlay.choosingDestination || (overlay.currentEvent && overlay.currentEvent.destinations && overlay.currentEvent.destinations.length > 0)))
                            onClicked: {
                                if (overlay.currentJob) overlay.closeJob()
                                else if (overlay.preview && overlay.preview.commitAvailable) { if (overlay.confirmRestart) overlay.startImport(); else overlay.confirmRestart = true }
                                else if (overlay.preview) overlay.closePreview()
                                else if (overlay.choosingDestination) overlay.choosingDestination = false
                                else if (overlay.currentEvent && overlay.currentEvent.destinations && overlay.currentEvent.destinations.length > 0) overlay.choosingDestination = true
                            }
                        }
                    }

                    Rectangle {
                        width: 132
                        height: 44
                        radius: 6
                        visible: overlay.currentJob === null
                        color: noMouse.pressed ? "#48525b" : "#343c44"
                        Text { anchors.centerIn: parent; text: overlay.preview ? "Abbrechen" : (overlay.choosingDestination ? "Zurück" : "Nein"); color: "white"; font.pixelSize: 16 }
                        MouseArea {
                            id: noMouse
                            anchors.fill: parent
                            enabled: !overlay.busy && !overlay.currentJob
                            onClicked: {
                                if (overlay.preview) overlay.closePreview()
                                else if (overlay.choosingDestination) overlay.choosingDestination = false
                                else overlay.respond("decline", "")
                            }
                        }
                    }
                }
            }
        }
    }

    onMediaImportBridgeChanged: { syncJobs(); syncEvent() }
}
