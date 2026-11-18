.pragma library

function fresh() { return { running: false, accumulated: 0, startedAt: 0 } }
function play(state, now) {
    if (!state.running) { state.startedAt = now; state.running = true }
    return state
}
function pause(state, now) {
    if (state.running) { state.accumulated += Math.max(0, now - state.startedAt); state.running = false }
    return state
}
function reset() { return fresh() }
function elapsed(state, now) { return state.accumulated + (state.running ? Math.max(0, now - state.startedAt) : 0) }
function formatElapsed(milliseconds) {
    var seconds = Math.floor(Math.max(0, milliseconds) / 1000)
    var hours = Math.floor(seconds / 3600)
    var minutes = Math.floor((seconds % 3600) / 60)
    var rest = seconds % 60
    function two(value) { return value < 10 ? "0" + value : "" + value }
    return two(hours) + ":" + two(minutes) + ":" + two(rest)
}
function formatClock(date) {
    function two(value) { return value < 10 ? "0" + value : "" + value }
    return two(date.getHours()) + ":" + two(date.getMinutes())
}
