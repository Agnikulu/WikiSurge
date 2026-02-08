import { useEffect, useRef, useState, useCallback } from 'react';

interface UseWebSocketOptions {
  url: string;
  filter?: Record<string, string>;
  onMessage?: (data: unknown) => void;
  reconnectDelay?: number;
  maxItems?: number;
}

export function useWebSocket<T>({
  url,
  filter,
  onMessage,
  reconnectDelay = 3000,
  maxItems = 100,
}: UseWebSocketOptions) {
  const [data, setData] = useState<T[]>([]);
  const [connected, setConnected] = useState(false);
  const ws = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const filterRef = useRef(filter);
  filterRef.current = filter;

  const connect = useCallback(() => {
    // Build URL with query params
    const wsUrl = new URL(url);
    if (filterRef.current) {
      Object.entries(filterRef.current).forEach(([key, value]) => {
        if (value) wsUrl.searchParams.set(key, value);
      });
    }

    ws.current = new WebSocket(wsUrl.toString());

    ws.current.onopen = () => {
      console.log('WebSocket connected');
      setConnected(true);
    };

    ws.current.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);

        if (onMessage) {
          onMessage(message);
        }

        setData((prev) => {
          const updated = [message.data as T, ...prev];
          return updated.slice(0, maxItems);
        });
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err);
      }
    };

    ws.current.onclose = () => {
      console.log('WebSocket disconnected');
      setConnected(false);

      // Reconnect after delay
      reconnectTimeout.current = setTimeout(() => {
        console.log('Reconnecting...');
        connect();
      }, reconnectDelay);
    };

    ws.current.onerror = (error) => {
      console.error('WebSocket error:', error);
    };
  }, [url, reconnectDelay, maxItems, onMessage]);

  useEffect(() => {
    connect();

    return () => {
      if (reconnectTimeout.current) {
        clearTimeout(reconnectTimeout.current);
      }
      ws.current?.close();
    };
  }, [connect]);

  const clearData = useCallback(() => setData([]), []);

  return { data, connected, clearData };
}
