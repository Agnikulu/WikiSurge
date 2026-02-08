import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EditsTimelineChart } from '../components/Stats/EditsTimelineChart';

// Mock recharts to avoid canvas/SVG issues in jsdom
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
  getStats: vi.fn(() =>
    Promise.resolve({
      edits_per_second: 5.2,
      edits_today: 100000,
      hot_pages_count: 50,
      trending_count: 30,
      active_alerts: 5,
    }),
  ),
}));

describe('EditsTimelineChart', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders chart heading', async () => {
    render(<EditsTimelineChart />);
    expect(screen.getByText('Edits Timeline')).toBeInTheDocument();
  });

  it('renders time range buttons', () => {
    render(<EditsTimelineChart />);
    expect(screen.getByText('1 Hour')).toBeInTheDocument();
    expect(screen.getByText('6 Hours')).toBeInTheDocument();
    expect(screen.getByText('24 Hours')).toBeInTheDocument();
  });

  it('1 Hour is active by default', () => {
    render(<EditsTimelineChart />);
    const btn = screen.getByText('1 Hour');
    expect(btn.className).toContain('bg-blue-600');
  });

  it('switches active range on click', async () => {
    const user = userEvent.setup();
    render(<EditsTimelineChart />);

    const sixHourBtn = screen.getByText('6 Hours');
    await user.click(sixHourBtn);

    expect(sixHourBtn.className).toContain('bg-blue-600');
    expect(screen.getByText('1 Hour').className).not.toContain('bg-blue-600');
  });

  it('shows collecting data message initially', async () => {
    render(<EditsTimelineChart />);
    await waitFor(() => {
      const msg = screen.queryByText(/Collecting data|Loading/);
      expect(msg).not.toBeNull();
    });
  });
});
