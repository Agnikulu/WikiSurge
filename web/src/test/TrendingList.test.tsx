import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import _userEvent from '@testing-library/user-event';
import { TrendingList } from '../components/Trending/TrendingList';
import type { TrendingPage } from '../types';

// Mock the API module
vi.mock('../utils/api', () => ({
  getTrending: vi.fn(),
}));

// Mock zustand store
vi.mock('../store/appStore', () => ({
  useAppStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector({
      updateTrending: vi.fn(),
      filters: { languages: [], excludeBots: false, minByteChange: 0 },
      setFilters: vi.fn(),
      resetFilters: vi.fn(),
    }),
}));

import { getTrending } from '../utils/api';
const mockedGetTrending = vi.mocked(getTrending);

const sampleTrending: TrendingPage[] = [
  {
    title: 'Climate change',
    score: 350.5,
    edits_1h: 42,
    last_edit: new Date().toISOString(),
    rank: 1,
    language: 'en',
  },
  {
    title: 'Artificial intelligence',
    score: 280.3,
    edits_1h: 31,
    last_edit: new Date().toISOString(),
    rank: 2,
    language: 'en',
  },
  {
    title: 'World Cup',
    score: 200.1,
    edits_1h: 25,
    last_edit: new Date().toISOString(),
    rank: 3,
    language: 'es',
  },
];

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  mockedGetTrending.mockResolvedValue(sampleTrending);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('TrendingList', () => {
  it('shows loading skeleton initially', () => {
    // Never resolve the promise so it stays loading
    mockedGetTrending.mockReturnValue(new Promise(() => {}));
    render(<TrendingList />);
    expect(screen.getByText('📈 Trending Pages')).toBeInTheDocument();
  });

  it('renders trending pages after fetch', async () => {
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByText('Climate change')).toBeInTheDocument();
    });

    expect(screen.getByText('Artificial intelligence')).toBeInTheDocument();
    expect(screen.getByText('World Cup')).toBeInTheDocument();
  });

  it('displays rank numbers', async () => {
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByText('#1')).toBeInTheDocument();
    });

    expect(screen.getByText('#2')).toBeInTheDocument();
    expect(screen.getByText('#3')).toBeInTheDocument();
  });

  it('shows error state on fetch failure', async () => {
    mockedGetTrending.mockRejectedValue(new Error('Network error'));
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByText('Failed to load trending pages')).toBeInTheDocument();
    });

    expect(screen.getByText('Retry')).toBeInTheDocument();
  });

  it('shows empty state when no trending pages', async () => {
    mockedGetTrending.mockResolvedValue([]);
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByText('No trending pages yet')).toBeInTheDocument();
    });
  });

  it('shows footer with page count', async () => {
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByText(/Showing top 3 pages/)).toBeInTheDocument();
    });
  });

  it('renders language filter buttons', async () => {
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByText('All')).toBeInTheDocument();
    });

    expect(screen.getByText('EN')).toBeInTheDocument();
    expect(screen.getByText('FR')).toBeInTheDocument();
  });

  it('has a manual refresh button', async () => {
    render(<TrendingList />);

    await waitFor(() => {
      expect(screen.getByLabelText('Refresh trending')).toBeInTheDocument();
    });
  });

  it('polls on interval and re-fetches', async () => {
    render(<TrendingList />);

    await waitFor(() => {
      expect(mockedGetTrending).toHaveBeenCalledTimes(1);
    });

    // Advance by 10s (polling interval)
    await act(async () => {
      vi.advanceTimersByTime(10_000);
    });

    // Should have been called a second time
    expect(mockedGetTrending.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
});
