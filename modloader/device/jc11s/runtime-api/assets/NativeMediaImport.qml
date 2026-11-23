import QtQuick 2.12
import QtQml 2.12
import Planck 1.0

// Embedded trusted runtime UI, never supplied by a mod package. Only the root broker can
// publish commands. Claim first, invoke once, report evidence; never retry calls.
Item {
    id: actor
    property var hostItem: defaultHost
    property string authToken: "__PRIME_UNLIMITED_AUTH_TOKEN__"
    property var jobs: []
    property bool jobsRefreshing: false
    property bool requesting: false
    property var pending: null
    property string lastHandledID: ""
    property var signalModel: null
    property var signalController: null
    property var registrationController: null
    QtObject {
        id: defaultHost
        property bool mainScreen: typeof planckQuickView === "undefined" ? true : planckQuickView.isMainScreen
    }
    function refreshJobs() {
        if (!isMainScreen() || jobsRefreshing) return
        jobsRefreshing = true
        var request = new XMLHttpRequest()
        request.timeout = 3000
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            jobsRefreshing = false
            if (request.status !== 200) return
            try { jobs = JSON.parse(request.responseText).jobs || [] } catch (error) {}
        }
        request.open("GET", "http://127.0.0.1:8098/api/v1/media-import/jobs")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        request.send()
    }

    Instantiator {
        id: deviceRows
        active: actor.isMainScreen() && model && model.rowCount() <= 32
        model: Planck.librarianController.devicesController.deviceModel
        delegate: QtObject { property var roles: model }
    }
    Instantiator {
        id: sourceRows
        active: actor.isMainScreen() && model && model.rowCount() <= 32
        model: Planck.librarianController.browserSourceControllerPtr().connectedSourceModel()
        delegate: QtObject { property int sourceRow: index; property var roles: model }
    }
    function isMainScreen() { return !!hostItem && hostItem.mainScreen === true }
    function safeID(value) {
        var n = Number(value)
        return /^[1-9][0-9]*$/.test(String(value)) && n <= 9007199254740991 && Math.floor(n) === n
    }
    function sameBinding(a, b) {
        return !!a && !!b && a.LibraryUUID === b.LibraryUUID &&
            a.MountIdentity === b.MountIdentity && a.EngineIdentity === b.EngineIdentity
    }
    function nativeRequest(job, action, body, done) {
        var request = new XMLHttpRequest()
        request.open(body === null ? "GET" : "POST", "http://127.0.0.1:8098/api/v1/media-import/native/" + job + "/" + action)
        request.timeout = 15000
        request.setRequestHeader("Content-Type", "application/json")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            var payload = null
            try { payload = JSON.parse(request.responseText) } catch (e) {}
            done(request.status, payload)
        }
        request.send(body === null ? "" : JSON.stringify(body))
    }
    function activeJob() {
        var found = ""
        for (var i = 0; i < jobs.length; i++) {
            var j = jobs[i]
            if (j.mode !== "live" || ["copying", "committing_library"].indexOf(j.phase) < 0) continue
            if (!/^[0-9a-f]{64}$/.test(j.id) || found !== "") return ""
            found = j.id
        }
        return found
    }
    function validEnvelope(e, job) {
        if (!e || !e.command || !isMainScreen()) return false
        var c = e.command
        return c.jobId === job && /^[0-9a-f]{64}$/.test(c.id) && !!c.binding &&
            /^[0-9a-f-]{36}$/.test(c.binding.LibraryUUID) && /^[0-9a-f]{64}$/.test(c.binding.MountIdentity) &&
            typeof c.binding.EngineIdentity === "string" && c.binding.EngineIdentity.length > 0 &&
            typeof e.sourceDevice === "string" && e.sourceDevice.indexOf("/media/") === 0 &&
            e.sourceDevice.indexOf("..") < 0 && typeof e.expiresUnixMs === "number" && e.expiresUnixMs > Date.now() &&
            ["select_library", "all_tracks", "register_files", "lookup_tracks", "create_playlist", "add_tracks"].indexOf(c.operation) >= 0
    }
    function target(e, allowLoading) {
        var lib = Planck.librarianController
        var devices = lib.devicesController
        var browser = lib.browserSourceControllerPtr()
        if (!browser || typeof browser.select !== "function" || deviceRows.count > 32 || sourceRows.count > 32) return null
        var devicesFound = 0
        for (var i = 0; i < deviceRows.count; i++) {
            var d = deviceRows.objectAt(i)
            if (!d || !d.roles || String(d.roles.path) !== e.sourceDevice) continue
            if (d.roles.readOnly === true || String(devices.getDatabaseUuid(e.sourceDevice)).toLowerCase() !== e.command.binding.LibraryUUID) return null
            devicesFound++
        }
        if (devicesFound !== 1) return null
        var found = null
        for (var j = 0; j < sourceRows.count; j++) {
            var s = sourceRows.objectAt(j)
            if (!s || !s.roles || String(s.roles.identifier) !== e.sourceDevice) continue
            if (found || (!allowLoading && s.roles.hasSpinner === true)) return null
            found = {controller: browser, index: browser.connectedSourceModel().index(s.sourceRow, 0)}
        }
        return found
    }
    function collection(e, allTracks, allowBusy) {
        if (!target(e, allowBusy)) return null
        var lib = Planck.librarianController
        var c = lib.currentCollectionController
        if (!c || c !== lib.collectionController || String(c.getDevicePath()) !== e.sourceDevice ||
            String(lib.devicesController.getCurrentDevicePath()) !== e.sourceDevice ||
            (!allowBusy && (c.isInserting || c.isCollectionJobRunning))) return null
        if (allTracks && ["0", "-1"].indexOf(String(c.getCraylistIdOfCurrentCollectionDisplayed())) < 0) return null
        return c
    }
    function rootTarget(e, allowBusy) {
        if (!collection(e, false, !!allowBusy)) return null
        var lib = Planck.librarianController
        var root = lib.devicesController.craylistModelRootModelIndex(e.sourceDevice)
        var controller = lib.craylistController
        if (!root || root.row < 0 || !root.model || !controller || String(controller.getSourceDevice(root)) !== e.sourceDevice) return null
        var model = root.model
        if (typeof model.rowCount !== "function" || typeof model.index !== "function" || typeof model.data !== "function" || model.fetchingContent === true) return null
        return {root: root, model: model, controller: controller}
    }
    function finish(outcome, extra) {
        var p = pending
        if (!p) return
        // Clear before HTTP: neither signal reentrancy nor an ambiguous reply
        // can cause a second acknowledgement or another native invocation.
        pending = null
        signalModel = null
        signalController = null
        registrationController = null
        var observation = {outcome: outcome, binding: p.envelope.command.binding}
        if (outcome === "applied") {
            try {
                var lib = Planck.librarianController
                observation.sourceDevice = String(lib.devicesController.getCurrentDevicePath())
                observation.collectionDevice = String(lib.currentCollectionController.getDevicePath())
                observation.libraryUuid = String(lib.devicesController.getDatabaseUuid(observation.sourceDevice)).toLowerCase()
                observation.allTracks = ["0", "-1"].indexOf(String(lib.currentCollectionController.getCraylistIdOfCurrentCollectionDisplayed())) >= 0
            } catch (error) { observation.outcome = "uncertain" }
        }
        if (extra) for (var k in extra) observation[k] = extra[k]
        nativeRequest(p.envelope.command.jobId, "observe", {id: p.envelope.command.id, observation: observation}, function(status, body) {})
    }
    function poll() {
        if (requesting || pending || !isMainScreen()) return
        var job = activeJob()
        if (!job) return
        requesting = true
        nativeRequest(job, "pending", null, function(status, e) {
            if (status !== 200 || !validEnvelope(e, job) || e.command.id === lastHandledID || activeJob() !== job) { requesting = false; return }
            lastHandledID = e.command.id
            nativeRequest(job, "claim", {id: e.command.id}, function(claimStatus, claimed) {
                requesting = false
                if (claimStatus !== 200 || !claimed || JSON.stringify(claimed) !== JSON.stringify(e)) return
                pending = {envelope: claimed, phase: "start", invoked: false}
                if (!validEnvelope(claimed, job) || activeJob() !== job) { finish("not_invoked"); return }
                start()
            })
        })
    }
    function beginListScan(p) {
        var t = rootTarget(p.envelope)
        if (!t) throw new Error("native list root unavailable")
        var count = t.model.rowCount(t.root)
        if (count < 0 || count > 100000) throw new Error("native root is too large")
        var rootID = String(t.model.data(t.root, ListsModel.UniqueIdRole))
        if (!/^-?[0-9]+$/.test(rootID)) throw new Error("native root identity unavailable")
        p.list = t
        p.rootID = rootID
        signalModel = t.model
        signalController = t.controller
        p.rows = count
        p.cursor = 0
        p.ids = ({})
        p.matches = []
        p.phase = "list_scan"
    }
    function start() {
        var p = pending
        if (!p) return
        try {
            var e = p.envelope, c = e.command, lib = Planck.librarianController
            var t = target(e)
            if (!t) throw new Error("native device changed")
            if (c.operation === "select_library") {
                if (collection(e, false, false)) { finish("applied"); return }
                p.phase = "select_wait"; p.invoked = true
                t.controller.select(t.index)
            } else if (c.operation === "all_tracks") {
                if (collection(e, true, false)) { finish("applied"); return }
                var root = rootTarget(e)
                if (!root || typeof root.controller.select !== "function") throw new Error("native root changed")
                p.phase = "all_wait"; p.invoked = true
                root.controller.select(root.root, 1, true, false)
            } else if (c.operation === "register_files") {
                var registration = collection(e, true, false)
                if (!registration || typeof registration.addTracksFromFileSystem !== "function" || !e.fileUrls || e.fileUrls.length !== c.paths.length || e.fileUrls.length < 1 || e.fileUrls.length > 512) throw new Error("registration context changed")
                for (var i = 0; i < e.fileUrls.length; i++) {
                    var expected = e.sourceDevice + "/Engine Library/" + c.paths[i]
                    if (c.paths[i].indexOf("Music/") !== 0 || c.paths[i].split("/").indexOf("..") >= 0 || decodeURIComponent(e.fileUrls[i]) !== "file://" + expected) throw new Error("copied URI differs")
                }
                p.registration = registration; registrationController = registration
                p.complete = false; p.phase = "register_wait"; p.invoked = true
                registration.addTracksFromFileSystem(e.fileUrls, -1)
            } else if (c.operation === "lookup_tracks") {
                var lookup = collection(e, true, false)
                if (!lookup || typeof lookup.getTrackUrisFromUniversalIds !== "function" || !lookup.collectionModel || !c.localIds || c.localIds.length < 1 || c.localIds.length > 512) throw new Error("lookup unavailable")
                p.lookup = lookup; p.model = lookup.collectionModel; p.rows = p.model.rowCount(); p.cursor = 0; p.found = ({}); p.wanted = ({})
                if (p.rows < 0 || p.rows > 1000000) throw new Error("native collection too large")
                for (var n = 0; n < c.localIds.length; n++) {
                    if (!safeID(c.localIds[n]) || c.localIds[n] >= 281474976710656 || p.wanted[String(c.localIds[n])]) throw new Error("invalid local IDs")
                    p.wanted[String(c.localIds[n])] = true
                }
                p.phase = "lookup_scan"
            } else beginListScan(p)
        } catch (error) { finish(p.invoked ? "uncertain" : "not_invoked", {reason: String(error).replace(/[^A-Za-z0-9_]/g, "_").slice(0, 128)}) }
    }
    function scanLists(p) {
        var e = p.envelope, c = e.command, t = rootTarget(e)
        if (!t || t.model !== p.list.model || t.root.row !== p.list.root.row || String(t.model.data(t.root, ListsModel.UniqueIdRole)) !== p.rootID || t.model.rowCount(t.root) !== p.rows) throw new Error("native list model changed")
        var began = Date.now(), count = 0
        while (p.cursor < p.rows && count++ < 200 && Date.now() - began < 4) {
            var index = t.model.index(p.cursor++, 0, t.root)
            var id = String(t.model.data(index, ListsModel.UniqueIdRole))
            if (!safeID(id) || p.ids[id]) throw new Error("ambiguous native playlist ID")
            p.ids[id] = true
            if (c.operation === "add_tracks" && c.playlist && id === c.playlist.NativeID && String(t.model.data(index, 0)) === c.playlist.Title) p.matches.push(index)
        }
        if (p.cursor < p.rows) return
        if (c.operation === "create_playlist") {
            if (typeof c.title !== "string" || c.title.trim() === "" || c.title.length > 512 || typeof t.controller.createList !== "function") throw new Error("invalid new playlist")
            p.inserted = []; p.phase = "create_wait"; p.invoked = true
            t.controller.createList(t.root, c.title)
        } else {
            if (p.matches.length !== 1 || !c.playlist || !sameBinding(c.playlist.Binding, c.binding) || !safeID(c.playlist.LocalID) || typeof t.controller.addTracksToList !== "function" || !c.tracks || !c.trackUris || c.tracks.length < 1 || c.tracks.length > 512 || c.trackUris.length !== c.tracks.length) throw new Error("target playlist or URI proofs differ")
            p.targetIndex = p.matches[0]; p.proofCursor = 0; p.phase = "add_proofs"
        }
    }
    function scanLookup(p) {
        var c = p.envelope.command
        var current = collection(p.envelope, true, false)
        if (current !== p.lookup || current.collectionModel !== p.model || p.model.rowCount() !== p.rows) throw new Error("native lookup context changed")
        var began = Date.now(), count = 0
        while (p.cursor < p.rows && count++ < 200 && Date.now() - began < 4) {
            var id = p.model.data(p.model.index(p.cursor++, 0), AbstractCollectionModel.TrackId)
            if (typeof id !== "number" || !safeID(id)) continue
            var local = String(id % 281474976710656)
            if (!p.wanted[local]) continue
            if (p.found[local]) throw new Error("ambiguous native track ID")
            p.found[local] = {id: String(id), uri: String(current.getTrackUrisFromUniversalIds([id]))}
        }
        if (p.cursor < p.rows) return
        var refs = [], uris = []
        for (var i = 0; i < c.localIds.length; i++) {
            var found = p.found[String(c.localIds[i])]
            if (!found) throw new Error("copied track missing from native model")
            refs.push({NativeID: found.id, LocalID: c.localIds[i], Binding: c.binding})
            uris.push(found.uri)
        }
        finish("applied", {tracks: refs, uris: uris})
    }
    function checkAddProofs(p) {
        var c = p.envelope.command, current = collection(p.envelope, false, false)
        var root = rootTarget(p.envelope)
        if (!current || typeof current.getTrackUrisFromUniversalIds !== "function" || !root || root.model !== p.list.model || root.model.rowCount(root.root) !== p.rows || String(root.model.data(p.targetIndex, ListsModel.UniqueIdRole)) !== c.playlist.NativeID || String(root.model.data(p.targetIndex, 0)) !== c.playlist.Title) throw new Error("native add context changed")
        var began = Date.now(), count = 0
        while (p.proofCursor < c.tracks.length && count++ < 50 && Date.now() - began < 4) {
            var i = p.proofCursor++, ref = c.tracks[i]
            if (!sameBinding(ref.Binding, c.binding) || !safeID(ref.NativeID) || !safeID(ref.LocalID) || String(current.getTrackUrisFromUniversalIds([Number(ref.NativeID)])) !== c.trackUris[i]) throw new Error("native track URI changed")
        }
        if (p.proofCursor < c.tracks.length) return
        var ids = [], seen = ({})
        for (var n = 0; n < c.tracks.length; n++) {
            var id = c.tracks[n].NativeID
            if (seen[id]) throw new Error("duplicate native track")
            seen[id] = true; ids.push(Number(id))
        }
        p.phase = "add_wait"; p.added = false; p.invoked = true
        root.controller.addTracksToList(p.targetIndex, ids)
    }
    function inserted(parent, first, last) {
        var p = pending
        if (!p || p.phase !== "create_wait") return
        // Native models can insert a provisional row and replace it while
        // refreshing. Read the settled root by title instead of adopting an ID
        // synchronously from a structural signal.
        p.insertSignal = true
    }
    function checkCreated(p) {
        if (!collection(p.envelope, false, true)) throw new Error("native creation source changed")
        var t = rootTarget(p.envelope, true)
        if (!t) return
        var rows = t.model.rowCount(t.root)
        if (rows < 0 || rows > 100000) throw new Error("native root exceeds scan boundary")
        if (rows === 0) return
        var now = Date.now()
        if (!p.creationScan || p.creationScan.model !== t.model || p.creationScan.rows !== rows) p.creationScan = {model:t.model, rows:rows, cursor:0, matches:[]}
        var scan = p.creationScan, count = 0
        while (scan.cursor < rows && count++ < 200 && Date.now() - now < 4) {
            var index = t.model.index(scan.cursor++, 0, t.root)
            if (String(t.model.data(index, 0)) !== p.envelope.command.title) continue
            var id = String(t.model.data(index, ListsModel.UniqueIdRole))
            if (!safeID(id)) { p.creationScan = null; return }
            scan.matches.push(id)
        }
        if (scan.cursor < rows) return
        p.creationScan = null
        if (scan.matches.length > 1) throw new Error("new native playlist title is ambiguous")
        if (scan.matches.length !== 1) return
        var id = scan.matches[0]
        if (p.stableCreatedID !== id) { p.stableCreatedID = id; p.stableCreatedAt = Date.now(); return }
        if (Date.now() - p.stableCreatedAt < 100) return
        finish("applied", {playlist:{NativeID:id, Binding:p.envelope.command.binding}})
    }
    function tracksAdded(index) {
        var p = pending
        if (!p || p.phase !== "add_wait") return
        try {
            if (index && index.model === p.list.model && String(p.list.controller.getSourceDevice(index)) === p.envelope.sourceDevice &&
                String(index.model.data(index, ListsModel.UniqueIdRole)) === p.envelope.command.playlist.NativeID) p.added = true
        } catch (error) { finish("uncertain") }
    }
    function collectionComplete() {
        if (pending && pending.phase === "register_wait") pending.complete = true
    }
    function advance() {
        var p = pending
        if (!p) return
        try {
            if (!validEnvelope(p.envelope, p.envelope.command.jobId) || activeJob() !== p.envelope.command.jobId || !target(p.envelope, p.invoked)) throw new Error("live command context changed")
            if (p.phase === "select_wait") {
                if (collection(p.envelope, false, false)) finish("applied")
            } else if (p.phase === "all_wait") {
                if (collection(p.envelope, true, false)) finish("applied")
            } else if (p.phase === "register_wait") {
                if (!collection(p.envelope, true, true) || Planck.librarianController.currentCollectionController !== p.registration) throw new Error("registration source changed")
                if (p.complete && collection(p.envelope, true, false)) finish("applied")
            } else if (p.phase === "lookup_scan") scanLookup(p)
            else if (p.phase === "list_scan") scanLists(p)
            else if (p.phase === "add_proofs") checkAddProofs(p)
            else if (p.phase === "create_wait") {
                checkCreated(p)
            } else if (p.phase === "add_wait") {
                if (!collection(p.envelope, false, true)) throw new Error("native add target changed")
                if (p.added && collection(p.envelope, false, false)) finish("applied")
            }
        } catch (error) { finish(p.invoked ? "uncertain" : "not_invoked", {reason:String(error).replace(/[^A-Za-z0-9_]/g,"_").slice(0,128)}) }
    }
    Connections {
        target: actor.signalModel
        ignoreUnknownSignals: true
        function onRowsInserted(parent, first, last) { actor.inserted(parent, first, last) }
    }
    Connections {
        target: actor.signalController
        ignoreUnknownSignals: true
        function onTracksAdded(index) { actor.tracksAdded(index) }
    }
    Connections {
        target: actor.registrationController
        ignoreUnknownSignals: true
        function onTrackCollectionComplete() { actor.collectionComplete() }
    }
    Timer { interval: 400; repeat: true; running: actor.isMainScreen(); onTriggered: actor.poll() }
    Timer { interval: 1000; repeat: true; triggeredOnStart: true; running: actor.isMainScreen(); onTriggered: actor.refreshJobs() }
    Timer { interval: 16; repeat: true; running: actor.pending !== null; onTriggered: actor.advance() }
}
