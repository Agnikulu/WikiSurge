import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LiveFeed } from '../components/LiveFeed/LiveFeed';
import type { Edit } from '../types';

// Mock WebSocket
class MockWebSocket {
  static instances: MockWebSocket[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  readyState = 0;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
    // Auto-connect after a tick
    setTimeout(() => {
      this.readyState = 1;
      this.onopen?.();
    }, 0);
  }

  close() {
    this.readyState = 3;
  }

  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  simulateClose() {
    this.readyState = 3;
    this.onclose?.();
  }
}

const now = Math.floor(Date.now() / 1000);

function makeEdit(overrides: Partial<Edit> = {}): Edit {
  return {
    id: Math.floor(Math.random() * 100000),
    type: 'edit',
    title: 'Test Page',
    user: 'TestUser',
    wiki: 'enwiki',
    bot: false,
    server_url: 'https://en.wikipedia.org',
    timestamp: now - 2,
    length: { old: 1000, new: 1100 },
    revision: { old: 50, new: 51 },
    comment: 'Test comment',
    ...overrides,
  };
}

describe('LiveFeed', () => {
  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.stubGlobal('WebSocket', MockWebSocket);
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('renders the title', () => {
    render(<LiveFeed />);
    expect(screen.getByText('Live Edit Feed')).toBeInTheDocument();
  });

  it('shows connecting state initially', () => {
    render(<LiveFeed />);
    const elements = screen.getAllByText(/Connecting/i);
    expect(elements.length).toBeGreaterThanOrEqual(1);
  });

  it('shows connected state after WebSocket connects', async () => {
    render(<LiveFeed />);
    await act(async () => {
      vi.advanceTimersByTime(10);
    });
    expect(screen.getByText('Live')).toBeInTheDocument();
  });

  it('shows waiting message when connected but no edits', async () => {
    render(<LiveFeed />);
    await act(async () => {
      vi.advanceTimersByTime(10);
    });
    expect(screen.getByText(/Waiting for edits/i)).toBeInTheDocument();
  });

  it('displays edits when WebSocket messages arrive', async () => {
    render(<LiveFeed />);
    await act(async () => {
      vi.advanceTimersByTime(10);
    });

    const ws = MockWebSocket.instances[0];
    const edit = makeEdit({ title: 'Wikipedia Article' });

    await act(async () => {
      ws.simulateMessage({ type: 'edit', data: edit });
      vi.advanceTimersByTime(200); // Wait for batch flush
    });

    expect(screen.getByText('Wikipedia Article')).toBeInTheDocument();
  });

  it('shows multiple edits', async () => {
    render(<LiveFeed />);
    await act(async () => {
      vi.advanceTimersByTime(10);
    });

    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateMessage({ type: 'edit', data: makeEdit({ title: 'Article 1' }) });
      ws.simulateMessage({ type: 'edit', data: makeEdit({ title: 'Article 2' }) });
      ws.simulateMessage({ type: 'edit', data: makeEdit({ title: 'Article 3' }) });
      vi.advanceTimersByTime(200);
    });

    expect(screen.getByText('Article 1')).toBeInTheDocument();
    expect(screen.getByText('Article 2')).toBeInTheDocument();
    expect(screen.getByText('Article 3')).toBeInTheDocument();
  });

  it('pause button stops new edits from appearing', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<LiveFeed />);

    await act(async () => {
      vi.advanceTimersByTime(10);
    });

    // Click pause
    const pauseBtn = screen.getByLabelText('Pause live feed');
    await user.click(pauseBtn);

    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateMessage({ type: 'edit', data: makeEdit({ title: 'Paused Edit' }) });
      vi.advanceTimersByTime(200);
    });

    // Should not appear because feed is paused
    expect(screen.queryByText('Paused Edit')).not.toBeInTheDocument();

    // Paused banner should be visible
    expect(screen.getByText(/Feed paused/i)).toBeInTheDocument();
  });

  it('clear button removes all edits', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<LiveFeed />);

    await act(async () => {
      vi.advanceTimersByTime(10);
    });

    const ws = MockWebSocket.instances[0];

    await act(async () => {
      ws.simulateMessage({ type: 'edit', data: makeEdit({ title: 'Clear Me' }) });
      vi.advanceTimersByTime(200);
    });

    expect(screen.getByText('Clear Me')).toBeInTheDocument();

    const clearBtn = screen.getByLabelText('Clear feed');
    await user.click(clearBtn);

    expect(screen.queryByText('Clear Me')).not.toBeInTheDocument();
  });

  it('filter toggle shows filter panel', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<LiveFeed />);

    const filterBtn = screen.getByLabelText('Toggle filters');
    await user.click(filterBtn);

    expect(screen.getByText('Filters')).toBeInTheDocument();
    expect(screen.getByText('Exclude bots')).toBeInTheDocument();
  });

  it('has proper feed ARIA role', () => {
    render(<LiveFeed />);
    expect(screen.getByRole('feed')).toBeInTheDocument();
  });
});
