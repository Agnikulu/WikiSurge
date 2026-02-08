import { useEffect, useRef, useState, useCallback } from 'react';

export type ConnectionState = 'connecting' | 'connected' | 'disconnected' | 'error';

interface UseWebSocketOptions {
  url: string;
  filter?: Record<string, string>;
  onMessage?: (data: unknown) => void;
  reconnectDelay?: number;
  maxItems?: number;
  maxRetries?: number;
}

interface UseWebSocketReturn<T> {
  data: T[];
  connectionState: ConnectionState;
  connected: boolean;
  reconnectCount: number;
  messageRate: number;
  clearData: () => void;
  pause: () => void;
  resume: () => void;
  isPaused: boolean;
}

export function useWebSocket<T>({
  url,
  filter,
  onMessage,
  reconnectDelay = 3000,
  maxItems = 100,
  maxRetries = 20,
}: UseWebSocketOptions): UseWebSocketReturn<T> {
  const [data, setData] = useState<T[]>([]);
  const [connectionState, setConnectionState] = useState<ConnectionState>('connecting');
  const [reconnectCount, setReconnectCount] = useState(0);
  const [messageRate, setMessageRate] = useState(0);
  const [isPaused, setIsPaused] = useState(false);

  const ws = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const filterRef = useRef(filter);
  const isPausedRef = useRef(isPaused);
  const retriesRef = useRef(0);
  const messageCountRef = useRef(0);
  const rateIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const bufferRef = useRef<T[]>([]);
  const flushTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  filterRef.current = filter;
  isPausedRef.current = isPaused;

  // Batch flush: accumulate messages and flush every 150ms
  const flushBuffer = useCallback(() => {
    if (bufferRef.current.length === 0) return;
    const batch = bufferRef.current;
    bufferRef.current = [];
    setData((prev) => {
      const updated = [...batch, ...prev];
      return updated.slice(0, maxItems);
    });
  }, [maxItems]);

  const scheduleFlush = useCallback(() => {
    if (flushTimeoutRef.current) return;
    flushTimeoutRef.current = setTimeout(() => {
      flushTimeoutRef.current = null;
      flushBuffer();
    }, 150);
  }, [flushBuffer]);

  const connect = useCallback(() => {
    setConnectionState('connecting');

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
      setConnectionState('connected');
      retriesRef.current = 0;
      setReconnectCount(0);
    };

    ws.current.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);
        messageCountRef.current++;

        if (onMessage) {
          onMessage(message);
        }

        if (!isPausedRef.current) {
          const editData = message.data as T;
          bufferRef.current.push(editData);
          scheduleFlush();
        }
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err);
      }
    };

    ws.current.onclose = () => {
      console.log('WebSocket disconnected');
      setConnectionState('disconnected');

      retriesRef.current++;
      setReconnectCount(retriesRef.current);

      if (retriesRef.current < maxRetries) {
        // Exponential backoff capped at 30s
        const delay = Math.min(reconnectDelay * Math.pow(1.5, retriesRef.current - 1), 30000);
        reconnectTimeout.current = setTimeout(() => {
          console.log(`Reconnecting (attempt ${retriesRef.current})...`);
          connect();
        }, delay);
      } else {
        setConnectionState('error');
      }
    };

    ws.current.onerror = (error) => {
      console.error('WebSocket error:', error);
      setConnectionState('error');
    };
  }, [url, reconnectDelay, maxRetries, maxItems, onMessage, scheduleFlush]);

  // Message rate tracker
  useEffect(() => {
    rateIntervalRef.current = setInterval(() => {
      setMessageRate(messageCountRef.current);
      messageCountRef.current = 0;
    }, 1000);

    return () => {
      if (rateIntervalRef.current) clearInterval(rateIntervalRef.current);
    };
  }, []);

  // Connect (and reconnect when filter changes)
  useEffect(() => {
    retriesRef.current = 0;
    setReconnectCount(0);
    connect();

    return () => {
      if (reconnectTimeout.current) {
        clearTimeout(reconnectTimeout.current);
      }
      if (flushTimeoutRef.current) {
        clearTimeout(flushTimeoutRef.current);
      }
      ws.current?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  const clearData = useCallback(() => {
    setData([]);
    bufferRef.current = [];
  }, []);

  const pause = useCallback(() => setIsPaused(true), []);
  const resume = useCallback(() => setIsPaused(false), []);

  return {
    data,
    connectionState,
    connected: connectionState === 'connected',
    reconnectCount,
    messageRate,
    clearData,
    pause,
    resume,
    isPaused,
  };
}
