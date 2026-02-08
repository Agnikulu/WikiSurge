import { useEffect, useRef, useCallback } from 'react';

interface UsePollingOptions {
  fetcher: () => Promise<void>;
  interval: number;
  enabled?: boolean;
}

/**
 * Hook that polls an API endpoint at a specified interval.
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
