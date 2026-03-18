import { useEffect, useRef, useState, useCallback } from "react";
import type { BuildWSMessage } from "@/types";

function getToken(): string {
  return localStorage.getItem("forgemill_token") || "";
}

export function useBuildWebSocket(buildId: number | null) {
  const [messages, setMessages] = useState<BuildWSMessage[]>([]);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectAttempts = useRef(0);
  const maxReconnectAttempts = 10;
  // N5: Track terminal state to prevent reconnects on finished builds
  const isTerminal = useRef(false);

  const connect = useCallback(() => {
    if (!buildId) return;
    // N5: Don't reconnect if build reached a terminal state
    if (isTerminal.current) return;

    const token = getToken();
    if (!token) return;

    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current);
      reconnectTimer.current = null;
    }

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    // H3: Use subprotocol instead of query parameter to avoid token leakage in logs
    const ws = new WebSocket(
      `${protocol}//${window.location.host}/api/ws/build/${buildId}`,
      [`token.${token}`]
    );

    ws.onopen = () => {
      setConnected(true);
      reconnectAttempts.current = 0;
    };
    ws.onclose = () => {
      setConnected(false);
      // 7.4: Reconnect with exponential backoff, but not for terminal states
      if (!isTerminal.current && reconnectAttempts.current < maxReconnectAttempts) {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttempts.current), 30000);
        reconnectAttempts.current++;
        reconnectTimer.current = setTimeout(connect, delay);
      }
    };
    // 7.5: Handle WebSocket errors
    ws.onerror = () => {
      setConnected(false);
    };
    ws.onmessage = (event) => {
      try {
        const msg: BuildWSMessage = JSON.parse(event.data);
        // N5: Mark terminal state on complete/error to stop reconnects
        if (msg.type === "complete" || msg.type === "error") {
          isTerminal.current = true;
        }
        setMessages((prev) => {
          const next = [...prev, msg];
          // Cap at 1000 messages to prevent unbounded memory growth
          if (next.length > 1000) return next.slice(-1000);
          return next;
        });
      } catch {
        // ignore malformed messages
      }
    };

    wsRef.current = ws;
  }, [buildId]);

  useEffect(() => {
    // F-148: Reset terminal state and messages when build ID changes,
    // otherwise the hook refuses to connect for a new build
    isTerminal.current = false;
    reconnectAttempts.current = 0;
    setMessages([]);
    connect();
    return () => {
      if (reconnectTimer.current) {
        clearTimeout(reconnectTimer.current);
      }
      reconnectAttempts.current = maxReconnectAttempts;
      wsRef.current?.close();
    };
  }, [connect]);

  return { messages, connected };
}
