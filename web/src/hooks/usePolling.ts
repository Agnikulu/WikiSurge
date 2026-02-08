import { useEffect, useRef, useCallback, useState } from 'react';

// ── Simple polling (fire-and-forget fetcher) ──────────────────────
interface UsePollingOptions {
  fetcher: () => Promise<void>;
  interval: number;
  enabled?: boolean;
}

/**
 * Hook that polls an API endpoint at a specified interval.
 * The caller owns the state; `fetcher` is expected to update it.
 */
export function usePolling({ fetcher, interval, enabled = true }: UsePollingOptions) {
  const savedFetcher = useRef(fetcher);
  savedFetcher.current = fetcher;

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const start = useCallback(() => {
    // Initial fetch
    savedFetcher.current();

    // Set up polling
    intervalRef.current = setInterval(() => {
      savedFetcher.current();
    }, interval);
  }, [interval]);

  const stop = useCallback(() => {
    if (intervalRef.current) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (enabled) {
      start();
    }
    return stop;
  }, [enabled, start, stop]);

  return { start, stop };
}

// ── Generic polling hook with managed state ───────────────────────
interface UsePollingDataOptions<T> {
  fetchFunction: () => Promise<T>;
  interval: number;
  enabled?: boolean;
}

interface UsePollingDataReturn<T> {
  data: T | null;
  loading: boolean;
  error: Error | null;
  refresh: () => Promise<void>;
  lastUpdate: Date | null;
}

/**
 * Generic polling hook that manages data / loading / error state.
 *
 * @example
 * const { data, loading, error, refresh } = usePollingData({
 *   fetchFunction: () => getTrending(20),
 *   interval: 10_000,
 * });
 */
export function usePollingData<T>({
  fetchFunction,
  interval,
  enabled = true,
}: UsePollingDataOptions<T>): UsePollingDataReturn<T> {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const [lastUpdate, setLastUpdate] = useState<Date | null>(null);

  const savedFn = useRef(fetchFunction);
  savedFn.current = fetchFunction;

  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const mountedRef = useRef(true);

  const execute = useCallback(async () => {
    try {
      setLoading(true);
      const result = await savedFn.current();
      if (mountedRef.current) {
        setData(result);
        setError(null);
        setLastUpdate(new Date());
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err instanceof Error ? err : new Error(String(err)));
      }
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }, []);

  // Start / stop polling
  useEffect(() => {
    mountedRef.current = true;

    if (!enabled) return;

    // Initial fetch
    execute();

    intervalRef.current = setInterval(execute, interval);

    return () => {
      mountedRef.current = false;
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [execute, interval, enabled]);

  return { data, loading, error, refresh: execute, lastUpdate };
}
