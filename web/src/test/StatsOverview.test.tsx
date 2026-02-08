import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { StatsOverview } from '../components/Stats/StatsOverview';
import type { Stats } from '../types';

// Mock the api module
const mockStats: Stats = {
  edits_per_second: 7.3,
  edits_today: 234567,
  hot_pages_count: 145,
  trending_count: 89,
  active_alerts: 12,
  top_language: 'en',
};

vi.mock('../utils/api', () => ({
  getStats: vi.fn(() => Promise.resolve(mockStats)),
}));

// Mock zustand store
vi.mock('../store/appStore', () => ({
  useAppStore: vi.fn((selector: (s: Record<string, unknown>) => unknown) =>
    selector({ updateStats: vi.fn() }),
  ),
}));

describe('StatsOverview', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('shows skeleton loaders while loading', () => {
    const { container } = render(<StatsOverview />);
    const skeletons = container.querySelectorAll('.animate-pulse');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it('renders all 6 stat cards after data loads', async () => {
    render(<StatsOverview />);
    await waitFor(() => {
      expect(screen.getByText('Edits/sec')).toBeInTheDocument();
    });

    expect(screen.getByText('Edits Today')).toBeInTheDocument();
    expect(screen.getByText('Hot Pages')).toBeInTheDocument();
    expect(screen.getByText('Trending')).toBeInTheDocument();
    expect(screen.getByText('Active Alerts')).toBeInTheDocument();
    expect(screen.getByText('Top Language')).toBeInTheDocument();
  });

  it('displays formatted stat values', async () => {
    render(<StatsOverview />);
    await waitFor(() => {
      expect(screen.getByText('7.3')).toBeInTheDocument();
    });

    expect(screen.getByText('234,567')).toBeInTheDocument();
    expect(screen.getByText('145')).toBeInTheDocument();
    expect(screen.getByText('89')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('en')).toBeInTheDocument();
  });

  it('renders the Overview heading', async () => {
    render(<StatsOverview />);
    await waitFor(() => {
      expect(screen.getByText('Overview')).toBeInTheDocument();
    });
  });

  it('renders last-updated timestamp', async () => {
    render(<StatsOverview />);
    await waitFor(() => {
      expect(screen.getByText(/Updated/)).toBeInTheDocument();
    });
  });

  it('renders a refresh button', async () => {
    render(<StatsOverview />);
    await waitFor(() => {
      expect(screen.getByLabelText('Refresh stats')).toBeInTheDocument();
    });
  });

  it('uses responsive grid classes', async () => {
    const { container } = render(<StatsOverview />);
    await waitFor(() => {
      expect(screen.getByText('Edits/sec')).toBeInTheDocument();
    });

    const grid = container.querySelector('.grid');
    expect(grid?.className).toContain('sm:grid-cols-2');
    expect(grid?.className).toContain('lg:grid-cols-3');
    expect(grid?.className).toContain('xl:grid-cols-6');
  });
});
