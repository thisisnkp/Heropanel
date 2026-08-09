// Realtime WebSocket client. A single shared connection multiplexes channel
// subscriptions (matching the backend hub, docs/04 §9). The session cookie
// authenticates the same-origin upgrade automatically.

type Handler = (data: unknown) => void;

let socket: WebSocket | null = null;
const handlers = new Map<string, Set<Handler>>();

function wsURL(): string {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  return `${proto}://${location.host}/api/v1/ws`;
}

function send(obj: unknown): void {
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(obj));
  }
}

function ensureSocket(): void {
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
    return;
  }
  const s = new WebSocket(wsURL());
  socket = s;

  s.onopen = () => {
    // (Re)subscribe to every active channel.
    const channels = Array.from(handlers.keys());
    if (channels.length > 0) send({ op: "subscribe", channels });
  };

  s.onmessage = (ev) => {
    let msg: { type?: string; channel?: string; data?: unknown };
    try {
      msg = JSON.parse(ev.data as string);
    } catch {
      return;
    }
    if (msg.type === "event" && msg.channel) {
      handlers.get(msg.channel)?.forEach((h) => h(msg.data));
    }
  };

  s.onclose = () => {
    socket = null;
    // Reconnect while there are active subscriptions.
    if (handlers.size > 0) setTimeout(ensureSocket, 1000);
  };

  s.onerror = () => {
    /* onclose handles reconnection */
  };
}

// wsSubscribe subscribes handler to a channel and returns an unsubscribe fn.
export function wsSubscribe(channel: string, handler: Handler): () => void {
  let set = handlers.get(channel);
  if (!set) {
    set = new Set();
    handlers.set(channel, set);
  }
  set.add(handler);

  ensureSocket();
  send({ op: "subscribe", channels: [channel] });

  return () => {
    const s = handlers.get(channel);
    if (!s) return;
    s.delete(handler);
    if (s.size === 0) {
      handlers.delete(channel);
      send({ op: "unsubscribe", channels: [channel] });
    }
  };
}
