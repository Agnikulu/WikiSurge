import { describe, it, expect, vi, afterEach, beforeAll } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EditsTimelineChart } from '../components/Stats/EditsTimelineChart';

// Stub ResizeObserver for jsdom
beforeAll(() => {
  global.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
});

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

const defaultMockStats = {
  edits_per_second: 5.2,
  edits_today: 100000,
  hot_pages_count: 50,
  trending_count: 30,
  active_alerts: 5,
};

// Mutable store state — tests can override before render
const mockStoreState: Record<string, unknown> = { stats: defaultMockStats };

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
  getTimeline: vi.fn(() => Promise.resolve([])),
}));

vi.mock('../store/appStore', () => ({
  useAppStore: (selector: (state: Record<string, unknown>) => unknown) =>
    selector(mockStoreState),
}));

describe('EditsTimelineChart', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders chart heading', async () => {
    render(<EditsTimelineChart />);
    expect(screen.getByText('EDITS MONITOR')).toBeInTheDocument();
  });

  it('renders time range buttons', () => {
    render(<EditsTimelineChart />);
    expect(screen.getByText('1H')).toBeInTheDocument();
    expect(screen.getByText('6H')).toBeInTheDocument();
    expect(screen.getByText('24H')).toBeInTheDocument();
  });

  it('1H is active by default', () => {
    render(<EditsTimelineChart />);
    const btn = screen.getByText('1H');
    // Active button uses inline style background, not bg-blue-600
    expect(btn.style.background).toBeTruthy();
  });

  it('switches active range on click', async () => {
    const user = userEvent.setup();
    render(<EditsTimelineChart />);

    const sixHourBtn = screen.getByText('6H');
    await user.click(sixHourBtn);

    expect(sixHourBtn.style.background).toBeTruthy();
  });

  it('shows collecting data message initially', async () => {
    // With stats=null the component shows "Acquiring signal…"
    mockStoreState.stats = null;
    render(<EditsTimelineChart />);
    await waitFor(() => {
      const msg = screen.queryByText(/Acquiring signal|Loading/i);
      expect(msg).not.toBeNull();
    });
    mockStoreState.stats = defaultMockStats; // restore
  });
});
