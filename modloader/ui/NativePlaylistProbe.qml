import QtQuick 2.12
import QtQml 2.12
import Planck 1.0

// Bounded inspection plus one durably claimed, internal-drive-only create test.
Item {
    id: probe
    property var hostItem: null
    property string authToken: ""
    property bool scanning: false
    property var queue: []
    property var observation: null

    property var createCandidates: []
    property var handledCommands: ({})
    property var pendingModel: null
    property string pendingCommandID: ""
    property bool commandBusy: false
    property var pendingController: null
    property var pendingListIndex: null
    property string pendingTargetListID: ""
    property var pendingRegistrationController: null
    property var lookupState: null

    Instantiator {
        id: nativeDeviceRows
        active: !!probe.hostItem && !!Planck.librarianController.devicesController.deviceModel && Planck.librarianController.devicesController.deviceModel.rowCount() <= 32
        model: Planck.librarianController.devicesController.deviceModel
        delegate: QtObject {
            property int deviceRow: index
            property var deviceRoles: model
        }
    }
    Instantiator {
        id: nativeConnectedRows
        model: Planck.librarianController.browserSourceControllerPtr().connectedSourceModel()
        active: !!probe.hostItem && model && model.rowCount() <= 32
        delegate: QtObject {
            property int sourceRow: index
            property var sourceRoles: model
        }
    }
    function nativeRequest(method, suffix, data, done) {
        var request = new XMLHttpRequest()
        request.open(method, "http://127.0.0.1:8098/api/v1/native-playlist/create-test" + suffix)
        request.setRequestHeader("Content-Type", "application/json")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        request.onreadystatechange = function() {
            if (request.readyState !== XMLHttpRequest.DONE) return
            var body = null
            try { body = JSON.parse(request.responseText) } catch (e) {}
            done(request.status, body)
        }
        request.send(data === null ? "" : JSON.stringify(data))
    }
    function acknowledgeCreate(id, stage) {
        nativeRequest("POST", "/ack", {id: id, stage: stage}, function(status, body) {})
    }
    function lookupContextMatches(command) {
        try {
            var lib = Planck.librarianController
            var c = lib.currentCollectionController
            return c === lib.collectionController && !c.isCollectionJobRunning && !c.isInserting &&
                String(c.getDevicePath()) === command.sourceDevice &&
                ["0", "-1"].indexOf(String(c.getCraylistIdOfCurrentCollectionDisplayed())) >= 0 &&
                String(lib.devicesController.getCurrentDevicePath()) === command.sourceDevice &&
                String(lib.devicesController.getDatabaseUuid(command.sourceDevice)).toLowerCase() === command.identity.libraryUuid &&
                typeof c.getTrackUrisFromUniversalIds === "function" && c.collectionModel &&
                typeof c.collectionModel.rowCount === "function" && typeof c.collectionModel.index === "function" &&
                typeof c.collectionModel.data === "function"
        } catch (e) { return false }
    }
    function beginNativeLookup(command) {
        if (!lookupContextMatches(command)) { acknowledgeCreate(command.id, "lookup_failed"); return }
        var model = Planck.librarianController.currentCollectionController.collectionModel
        lookupState = {command: command, model: model, cursor: 0, rows: model.rowCount()}
    }
    function scanNativeLookupChunk() {
        var state = lookupState
        if (!state) return
        try {
            if (!lookupContextMatches(state.command) || Planck.librarianController.currentCollectionController.collectionModel !== state.model || state.rows !== state.model.rowCount() || state.rows > 1000000) throw new Error("lookup context changed")
            var started = Date.now()
            var processed = 0
            while (state.cursor < state.rows && processed < 200 && Date.now() - started < 4) {
                var index = state.model.index(state.cursor++, 0)
                processed++
                var id = state.model.data(index, AbstractCollectionModel.TrackId)
                // A bounded prefilter only: the ID is read from the native model,
                // then the server independently checks its actual native URI.
                if (typeof id !== "number" || id <= 0 || id > 9007199254740991 || Math.floor(id) !== id || id % 281474976710656 !== state.command.lookupLocalId) continue
                var raw = String(Planck.librarianController.currentCollectionController.getTrackUrisFromUniversalIds([id]))
                lookupState = null
                nativeRequest("POST", "/ack", {id: state.command.id, stage: "lookup_result", nativeTrackId: String(id), nativeUri: raw}, function(status, result) {})
                return
            }
            if (state.cursor >= state.rows) throw new Error("track not found in current native model")
        } catch (e) {
            lookupState = null
            acknowledgeCreate(state.command.id, "lookup_failed")
        }
    }
    Timer {
        interval: 16
        repeat: true
        running: probe.lookupState !== null
        onTriggered: probe.scanNativeLookupChunk()
    }
    function nativeDeviceTarget(command) {
        var librarian = Planck.librarianController
        var devices = librarian.devicesController
        var browser = librarian.browserSourceControllerPtr()
        if (!browser || typeof browser.select !== "function") return null
        var matchedDevice = 0
        for (var i = 0; i < Math.min(32, nativeDeviceRows.count); i++) {
            var row = nativeDeviceRows.objectAt(i)
            if (!row || !row.deviceRoles || String(row.deviceRoles.path) !== command.sourceDevice) continue
            if (String(devices.getDatabaseUuid(command.sourceDevice)).toLowerCase() !== command.identity.libraryUuid || row.deviceRoles.readOnly === true) return null
            matchedDevice++
        }
        if (matchedDevice !== 1) return null
        var found = null
        for (var j = 0; j < Math.min(32, nativeConnectedRows.count); j++) {
            var source = nativeConnectedRows.objectAt(j)
            if (!source || !source.sourceRoles || String(source.sourceRoles.identifier) !== command.sourceDevice) continue
            if (found || source.sourceRoles.hasSpinner === true) return null
            found = {index: browser.connectedSourceModel().index(source.sourceRow, 0), controller: browser}
        }
        return found
    }
    function createTargetMatches(item, command) {
        try {
            if (command.operation === "lookup") return lookupContextMatches(command)
            if (command.operation === "select") return nativeDeviceTarget(command) !== null
            if (command.operation === "all_tracks") {
                var lib = Planck.librarianController
                var device = lib.devicesController
                var rootIndex = device.craylistModelRootModelIndex(command.sourceDevice)
                return nativeDeviceTarget(command) !== null && String(device.getCurrentDevicePath()) === command.sourceDevice &&
                    lib.currentCollectionController === lib.collectionController && !lib.collectionController.isCollectionJobRunning &&
                    rootIndex && rootIndex.row >= 0 && String(lib.craylistController.getSourceDevice(rootIndex)) === command.sourceDevice
            }
            if (command.operation && ["add", "register"].indexOf(command.operation) < 0) return false
            if (command.operation === "register") {
                var library = Planck.librarianController
                var current = library.currentCollectionController
                if (!current || current !== library.collectionController || typeof current.addTracksFromFileSystem !== "function" || current.isInserting || current.isCollectionJobRunning ||
                    typeof command.fileUrl !== "string" || command.fileUrl.indexOf("file:///media/Denon/Engine%20Library/Music/USB%20Copy%20Native%20Test%20-%20") !== 0) return false
            }
            if (command.operation === "add" || command.operation === "register") {
                if (!item || !item.currentModelIndex || item.currentModelIndex.row !== command.currentRow ||
                    String(item.model.data(item.currentModelIndex, ListsModel.UniqueIdRole)) !== command.targetListId ||
                    String(item.model.data(item.currentModelIndex, 0)) !== command.title ||
                    command.title !== "USB Copy Live Test 20261122-124235 - 88487aa6" ||
                    typeof item.controller.addTracksToList !== "function") return false
            }
            return !!hostItem && hostItem.mainScreen && !!item && item.visible && item.enabled &&
                !!item.model && !!item.rootModelIndex && item.rootModelIndex.row === command.rootRow &&
                typeof item.controller.createList === "function" &&
                String(item.controller.getSourceDevice(item.rootModelIndex)) === command.sourceDevice
        } catch (e) { return false }
    }
    function pollNativeCreate() {
        if (commandBusy) return
        commandBusy = true
        nativeRequest("GET", "/pending", null, function(status, command) {
            if (status !== 200 || !command || handledCommands[command.id] ||
                !createTargetMatches(createCandidates[0], command)) { commandBusy = false; return }
            var item = createCandidates[0]
            // Mark before claiming: network ambiguity never causes an in-memory retry.
            handledCommands[command.id] = true
            nativeRequest("POST", "/claim", {id: command.id}, function(claimStatus, claimed) {
                if (claimStatus !== 200 || !claimed || claimed.id !== command.id) { commandBusy = false; return }
                if (!createTargetMatches(item, claimed)) {
                    acknowledgeCreate(command.id, "not_invoked")
                    commandBusy = false
                    return
                }
                pendingCommandID = command.id
                if (claimed.operation === "add") {
                    pendingController = item.controller
                    pendingListIndex = item.currentModelIndex
                    pendingTargetListID = claimed.targetListId
                    pendingModel = null
                } else if (claimed.operation === "select" || claimed.operation === "all_tracks" || claimed.operation === "lookup") {
                    pendingModel = null
                    pendingController = null
                } else if (claimed.operation === "register") {
                    pendingRegistrationController = Planck.librarianController.currentCollectionController
                    pendingModel = pendingRegistrationController.collectionModel
                } else pendingModel = item.model
                Qt.callLater(function() {
                    if (!createTargetMatches(item, claimed)) {
                        acknowledgeCreate(command.id, "not_invoked")
                        pendingModel = null
                        commandBusy = false
                        return
                    }
                    try {
                        if (claimed.operation === "add") item.controller.addTracksToList(item.currentModelIndex, [Number(claimed.nativeTrackId)])
                        else if (claimed.operation === "select") {
                            var librarian = Planck.librarianController
                            var target = nativeDeviceTarget(claimed)
                            if (!target) throw new Error("native target changed")
                            target.controller.select(target.index)
                        }
                        else if (claimed.operation === "all_tracks") {
                            var lib = Planck.librarianController
                            var rootIndex = lib.devicesController.craylistModelRootModelIndex(claimed.sourceDevice)
                            lib.craylistController.select(rootIndex, 1, true, false)
                        }
                        else if (claimed.operation === "lookup") beginNativeLookup(claimed)
                        else if (claimed.operation === "register") pendingRegistrationController.addTracksFromFileSystem([claimed.fileUrl], -1)
                        else item.controller.createList(item.rootModelIndex, claimed.title)
                        acknowledgeCreate(command.id, "submitted")
                    } catch (e) { acknowledgeCreate(command.id, "uncertain") }
                    commandBusy = false
                })
            })
        })
    }
    Connections {
        target: probe.pendingModel
        ignoreUnknownSignals: true
        function onRowsInserted() {
            if (probe.pendingCommandID.length === 64) {
                probe.acknowledgeCreate(probe.pendingCommandID, "model_rows_inserted")
                probe.pendingModel = null
            }
        }
    }
    Connections {
        target: probe.pendingController
        ignoreUnknownSignals: true
        function onTracksAdded(index) {
            if (probe.pendingCommandID.length === 64 && probe.pendingListIndex && index &&
                index.model === probe.pendingListIndex.model && index.row === probe.pendingListIndex.row &&
                String(index.model.data(index, ListsModel.UniqueIdRole)) === probe.pendingTargetListID) {
                probe.acknowledgeCreate(probe.pendingCommandID, "model_rows_inserted")
                probe.pendingController = null
            }
        }
    }
    Connections {
        target: probe.pendingRegistrationController
        ignoreUnknownSignals: true
        function onTrackCollectionComplete() {
            if (probe.pendingCommandID.length === 64) probe.acknowledgeCreate(probe.pendingCommandID, "collection_complete")
        }
    }
    function typeName(value) {
        var type = typeof value
        return ["object", "number", "string", "undefined", "boolean"].indexOf(type) >= 0 ? type : "undefined"
    }
    function observeID(value) {
        if (value === undefined || value === null || observation.ids.length >= 6) return
        var raw = String(value)
        var safe = /^[0-9a-fA-F:{};-]{1,128}$/.test(raw) ? raw : ""
        for (var i = 0; i < observation.ids.length; i++) {
            var previous = observation.ids[i]
            if (previous.type === typeName(value) && previous.value === safe && previous.length === raw.length) return
        }
        observation.ids.push({type: typeName(value), value: safe, length: Math.min(raw.length, 100000)})
    }
    function beginScan() {
        createCandidates = []
        var root = hostItem
        var depth = 0
        while (root && root.parent && depth < 64) { root = root.parent; depth++ }
        observation = {devices: [], mainScreen: hostItem.mainScreen, visited: 0, truncated: false, durationMs: 0, rootDepth: depth, canRefreshCollection: false, canImportTracks: false, canAddTracksToDatabase: false, instances: [], ids: [], errors: 0, visibleVisited: 0, classHints: []}
        try {
            var librarian = Planck.librarianController
            var collection = librarian.currentCollectionController
            try {
                var devices = librarian.devicesController
                var deviceModel = devices.deviceModel
                observation.classHints.push("Probe_DeviceRoles_" + (deviceModel ? typeof deviceModel.getRoles : "undefined"))
                observation.classHints.push("Probe_DeviceRows_" + (deviceModel ? typeof deviceModel.rowCount : "undefined"))
                observation.classHints.push("Probe_CurrentDenon_" + (String(devices.getCurrentDevicePath()) === "/media/Denon"))
                for (var sr = 0; sr < Math.min(32, nativeConnectedRows.count); sr++) {
                    var sourceRow = nativeConnectedRows.objectAt(sr)
                    var sourceKeys = sourceRow ? Object.keys(sourceRow.sourceRoles) : []
                    if (sourceRow && String(sourceRow.sourceRoles.identifier) === "/media/Denon") observation.classHints.push("Probe_BrowserDenonCurrent_" + !!sourceRow.sourceRoles.isCurrent)
                    if (sourceRow && String(sourceRow.sourceRoles.identifier) === "/media/MP3 2") observation.classHints.push("Probe_BrowserUSBCurrent_" + !!sourceRow.sourceRoles.isCurrent)
                    if (sr === 0) observation.classHints.push(("Probe_SourceRoleNames_" + sourceKeys.join("_")).replace(/[^A-Za-z0-9_]/g, "_").slice(0,128))
                    for (var sk = 0; sk < sourceKeys.length; sk++) {
                        if (String(sourceRow.sourceRoles[sourceKeys[sk]]) === "/media/Denon") {
                            observation.classHints.push("Probe_SourceDenonRow_" + sourceRow.sourceRow)
                            observation.classHints.push("Probe_SourcePathRole_" + sourceKeys[sk])
                        }
                    }
                }
                for (var nr = 0; nr < Math.min(32, nativeDeviceRows.count); nr++) {
                    var nativeRow = nativeDeviceRows.objectAt(nr)
                    if (!nativeRow || !nativeRow.deviceRoles) continue
                    var devicePath = String(nativeRow.deviceRoles.path)
                    if (devicePath.indexOf("/media/") === 0 && devicePath.length <= 256 && devicePath.indexOf("..") < 0) {
                        var deviceUUID = String(devices.getDatabaseUuid(devicePath)).toLowerCase()
                        if (/^[0-9a-fA-F:{};-]{0,128}$/.test(deviceUUID)) observation.devices.push({path: devicePath, uuid: deviceUUID, row: nativeRow.deviceRow, current: String(devices.getCurrentDevicePath()) === devicePath, canSelect: typeof devices.select === "function" && typeof librarian.sourceViewIndex === "number" && nativeRow.deviceRoles.readOnly !== true})
                    }
                    var nativeKeys = Object.keys(nativeRow.deviceRoles)
                    if (nr === 0) observation.classHints.push(("Probe_DeviceRoleNames_" + nativeKeys.join("_")).replace(/[^A-Za-z0-9_]/g, "_").slice(0, 128))
                    for (var nk = 0; nk < nativeKeys.length; nk++) {
                        if (String(nativeRow.deviceRoles[nativeKeys[nk]]) === "/media/Denon") {
                            observation.classHints.push("Probe_DenonRow_" + nativeRow.deviceRow)
                            observation.classHints.push("Probe_DevicePathRole_" + nativeKeys[nk])
                        }
                    }
                }
                if (deviceModel && typeof deviceModel.getRoles === "function" && typeof deviceModel.rowCount === "function" && typeof deviceModel.index === "function") {
                    var count = Math.min(16, deviceModel.rowCount())
                    for (var dr = 0; dr < count; dr++) {
                        var deviceRoles = deviceModel.getRoles(deviceModel.index(dr, 0))
                        var keys = Object.keys(deviceRoles)
                        for (var dk = 0; dk < keys.length; dk++) {
                            if (String(deviceRoles[keys[dk]]) === "/media/Denon") {
                                observation.classHints.push("Probe_DenonRow_" + dr)
                                if (/^[A-Za-z0-9_]{1,64}$/.test(keys[dk])) observation.classHints.push("Probe_DevicePathRole_" + keys[dk])
                            }
                        }
                    }
                }
            } catch (e) {}

            observation.collectionIsLibrary = !!collection && collection === librarian.collectionController
            observation.canRegisterFiles = !!collection && typeof collection.addTracksFromFileSystem === "function" && !collection.isInserting && !collection.isCollectionJobRunning
            observation.canRefreshCollection = !!collection && typeof collection.refreshDisplayedCollection === "function"
            observation.canImportTracks = typeof librarian.importTracks === "function"
            observation.canAddTracksToDatabase = typeof librarian.addTracksToDatabase === "function"
            observation.classHints.push("Probe_AddFile_" + (collection ? typeof collection.addTracksFromFileSystem : "undefined"))
            observation.classHints.push("Probe_ReimportMetadata_" + typeof librarian.reimportTracksMetadata)
            var model = collection ? collection.collectionModel : null
            if (model) {
                var modelClass = String(model).split("(")[0]
                if (/^[A-Za-z0-9_]{1,128}$/.test(modelClass)) observation.classHints.push(modelClass)
            }
            observation.collectionPath = ""
            observation.collectionListId = ""
            observation.collectionRows = 0
            try {
                if (collection && typeof collection.getDevicePath === "function") {
                    var collectionPath = String(collection.getDevicePath())
                    if (collectionPath.indexOf("/media/") === 0 && collectionPath.length <= 256 && collectionPath.indexOf("..") < 0) observation.collectionPath = collectionPath
                }
                if (collection && typeof collection.getCraylistIdOfCurrentCollectionDisplayed === "function") {
                    var listID = collection.getCraylistIdOfCurrentCollectionDisplayed()
                    if (typeof listID === "number" && Math.abs(listID) <= 9007199254740991) observation.collectionListId = String(listID)
                }
                if (model && typeof model.rowCount === "function") observation.collectionRows = Math.max(0, Math.min(1000000, model.rowCount()))
            } catch (e) {}
            var selection = collection ? collection.collectionSelectionModel : null
            var index = selection ? selection.currentIndex : null
            var role = typeof AbstractCollectionModel !== "undefined" ? AbstractCollectionModel.TrackId : undefined
            observation.classHints.push("Probe_ModelRoles_" + (model ? typeof model.getRoles : "undefined"))
            observation.classHints.push("Probe_ModelData_" + (model ? typeof model.data : "undefined"))
            observation.classHints.push("Probe_IndexRow_" + (index ? typeof index.row : "undefined"))
            observation.classHints.push("Probe_TrackIdRole_" + typeof role)
            if (model && index && typeof index.row === "number" && index.row >= 0) {
                if (typeof model.getRoles === "function") {
                    var roles = model.getRoles(index)
                    if (roles && roles.trackId !== undefined) observeID(roles.trackId)
                    else if (roles && roles.uniqueID !== undefined) observeID(roles.uniqueID)
                }
                if (observation.ids.length === 0 && typeof model.data === "function" && typeof role === "number") observeID(model.data(index, role))
            }
        } catch (e) {}
        queue = [{item: root, depth: 0}]
        scanning = true
    }
    function scanChunk() {
        var started = Date.now()
        var processed = 0
        while (queue.length && observation.visited < 20000 && processed < 300 && Date.now() - started < 4) {
            var entry = queue.pop()
            var item = entry.item
            processed++
            if (!item || entry.depth > 64) continue
            observation.visited++
            // Always enqueue children before inspecting optional properties.
            try {
                var children = item.children
                if (children) for (var i = 0; i < children.length; i++) queue.push({item: children[i], depth: entry.depth + 1})
            } catch (e) { observation.errors++ }
            try {
                var className = String(item).split("(")[0]
                if (/^[A-Za-z0-9_]{1,128}$/.test(className) && /Cray|Browse|TrackList|Collection/.test(className) && observation.classHints.indexOf(className) < 0 && observation.classHints.length < 32) observation.classHints.push(className)
                if (item.objectName === "Craylist" || /^Craylist(?:_|$)/.test(className)) {
                    if (observation.instances.length < 16) {
                        var controller = item.controller
                        var rootIndex = item.rootModelIndex
                        var currentIndex = item.currentModelIndex
                        var rootRow = rootIndex && typeof rootIndex.row === "number" ? rootIndex.row : -2
                        var currentRow = currentIndex && typeof currentIndex.row === "number" ? currentIndex.row : -2
                        var sourceDevice = ""
                        var sourceIndex = rootRow >= 0 ? rootIndex : (currentRow >= 0 ? currentIndex : null)
                        if (controller && sourceIndex && typeof controller.getSourceDevice === "function") {
                            var path = String(controller.getSourceDevice(sourceIndex))
                            if (path.indexOf("/media/") === 0 && path.length <= 256 && path.indexOf("..") < 0) sourceDevice = path
                        }
                        if (item.visible && item.enabled && controller && typeof controller.createList === "function" && rootRow >= 0 && sourceDevice !== "") createCandidates.push(item)
                        var selectedListID = ""
                        var selectedTestPlaylist = false
                        try {
                            if (item.model && currentRow >= 0 && typeof item.model.data === "function") {
                                var rawListID = String(item.model.data(currentIndex, ListsModel.UniqueIdRole))
                                if (/^[0-9]{1,16}$/.test(rawListID)) selectedListID = rawListID
                                selectedTestPlaylist = String(item.model.data(currentIndex, 0)) === "USB Copy Live Test 20261122-124235 - 88487aa6"
                            }
                        } catch (e) {}
                        observation.instances.push({selectedListID: selectedListID, selectedTestPlaylist: selectedTestPlaylist, sourceDevice: sourceDevice, rootRow: rootRow, currentRow: currentRow, visible: !!item.visible, enabled: !!item.enabled, controllerPresent: !!controller, modelPresent: !!item.model, canCreate: !!controller && typeof controller.createList === "function", canAdd: !!controller && typeof controller.addTracksToList === "function", canReadStructure: !!controller && typeof controller.reReadStructure === "function", rootIndexType: typeName(item.rootModelIndex), currentIndexType: typeName(item.currentModelIndex)})
                    }
                }
            } catch (e) { observation.errors++ }
            try {
                if (item.visible) {
                    observation.visibleVisited++
                    if (observation.ids.length < 6) {
                        var metadata = item.modelData
                        if (metadata && metadata.uniqueID !== undefined) observeID(metadata.uniqueID)
                        else if (metadata && metadata.trackId !== undefined) observeID(metadata.trackId)
                    }
                }
            } catch (e) { observation.errors++ }
        }
        observation.durationMs += Date.now() - started
        if (queue.length && observation.visited < 20000) return
        observation.truncated = queue.length > 0
        queue = []
        scanning = false
        var request = new XMLHttpRequest()
        request.open("POST", "http://127.0.0.1:8098/api/v1/native-playlist/probe/observation")
        request.setRequestHeader("Content-Type", "application/json")
        request.setRequestHeader("Authorization", "Bearer " + authToken)
        request.onreadystatechange = function() {
            if (request.readyState === XMLHttpRequest.DONE && request.status === 200) pollNativeCreate()
        }
        request.send(JSON.stringify(observation))
    }
    Timer {
        interval: probe.scanning ? 50 : 3000
        repeat: true
        running: probe.hostItem !== null && probe.hostItem.mainScreen && probe.authToken.length === 64
        onTriggered: {
            if (!probe.scanning) probe.beginScan()
            probe.scanChunk()
        }
    }
}
