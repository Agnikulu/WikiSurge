import { useState, useEffect, useCallback } from 'react';

interface UseAPIOptions<T> {
  fetcher: () => Promise<T>;
  initialData?: T;
  autoFetch?: boolean;
}

interface UseAPIResult<T> {
  data: T | undefined;
  loading: boolean;
  error: string | null;
  refetch: () => Promise<void>;
}

export function useAPI<T>({
  fetcher,
  initialData,
  autoFetch = true,
}: UseAPIOptions<T>): UseAPIResult<T> {
  const [data, setData] = useState<T | undefined>(initialData);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refetch = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await fetcher();
      setData(result);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'An error occurred';
      setError(message);
      console.error('API fetch error:', err);
    } finally {
      setLoading(false);
    }
  }, [fetcher]);

  useEffect(() => {
    if (autoFetch) {
      refetch();
    }
  }, [autoFetch, refetch]);

  return { data, loading, error, refetch };
}
