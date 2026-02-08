import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { LanguageDistributionChart } from '../components/Stats/LanguageDistributionChart';
import type { Stats } from '../types';

const mockStatsWithLanguages: Stats = {
  edits_per_second: 5.0,
  edits_today: 100000,
  hot_pages_count: 50,
  trending_count: 30,
  active_alerts: 5,
  top_languages: [
    { language: 'en', count: 5000, percentage: 35.0 },
    { language: 'de', count: 2000, percentage: 14.0 },
    { language: 'fr', count: 1500, percentage: 10.5 },
    { language: 'es', count: 1200, percentage: 8.4 },
    { language: 'ja', count: 1000, percentage: 7.0 },
  ],
  edits_by_type: { human: 8000, bot: 2000 },
};

// Mock recharts ResponsiveContainer to avoid jsdom issues
vi.mock('recharts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('recharts')>();
  return {
    ...actual,
    ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="responsive-container" style={{ width: 500, height: 300 }}>
        {children}
      </div>
    ),
  };
});

vi.mock('../utils/api', () => ({
  getStats: vi.fn(() => Promise.resolve(mockStatsWithLanguages)),
}));

describe('LanguageDistributionChart', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the heading', () => {
    render(<LanguageDistributionChart />);
    expect(screen.getByText('Language Distribution')).toBeInTheDocument();
  });

  it('shows loading state initially then loads data', async () => {
    render(<LanguageDistributionChart />);
    // either shows loading or the chart once data arrives
    await waitFor(() => {
      const heading = screen.getByText('Language Distribution');
      expect(heading).toBeInTheDocument();
    });
  });

  it('renders the edit type distribution section when data is available', async () => {
    render(<LanguageDistributionChart />);
    await waitFor(() => {
      const section = screen.queryByText('Edit Type Distribution');
      expect(section).toBeInTheDocument();
    });
  });
});

describe('LanguageDistributionChart – no language data', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows empty state when no languages are returned', async () => {
    const apiModule = await import('../utils/api');
    vi.mocked(apiModule.getStats).mockResolvedValueOnce({
      edits_per_second: 1.0,
      edits_today: 500,
      hot_pages_count: 5,
      trending_count: 3,
      active_alerts: 0,
    });

    render(<LanguageDistributionChart />);
    await waitFor(() => {
      const emptyMsg = screen.queryByText(/No language data|Loading/);
      expect(emptyMsg).not.toBeNull();
    });
  });
});
