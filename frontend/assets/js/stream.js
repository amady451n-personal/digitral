// Koneksi SSE /stream. EventSource mencoba tersambung ulang sendiri.

const STREAM_URL = "/stream";
const RECONNECT_MS = 5000;

let source = null;
let reconnectTimer = null;
let callbacks = null;

function scheduleReconnect() {
  if (reconnectTimer !== null) {
    return;
  }
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, RECONNECT_MS);
}

function onMessage(event) {
  let payload = null;
  try {
    payload = JSON.parse(event.data);
  } catch {
    return;
  }
  if (payload && Array.isArray(payload.numbers)) {
    callbacks.onSnapshot(payload);
  }
}

function onError() {
  callbacks.onState("offline");
  if (source && source.readyState === EventSource.CLOSED) {
    scheduleReconnect();
  }
}

function connect() {
  source?.close();
  source = new EventSource(STREAM_URL);
  source.addEventListener("open", () => callbacks.onState("live"));
  source.addEventListener("error", onError);
  source.addEventListener("message", onMessage);
}

export function startStream(options) {
  callbacks = options;
  callbacks.onState("connecting");
  connect();
}
