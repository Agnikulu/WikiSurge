import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AlertsPanel } from '../components/Alerts/AlertsPanel';
import type { SpikeAlert, EditWarAlert } from '../types';

// ── Mocks ──

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
}

vi.mock('../utils/api', () => ({
  getAlerts: vi.fn().mockResolvedValue([]),
}));

vi.mock('../utils/alertSounds', () => ({
  playCriticalAlert: vi.fn(),
  playEditWarAlert: vi.fn(),
  setAlertSoundsEnabled: vi.fn(),
  isAlertSoundsEnabled: vi.fn().mockReturnValue(false),
}));

import { getAlerts } from '../utils/api';
const mockedGetAlerts = vi.mocked(getAlerts);

const spikeAlert: SpikeAlert = {
  type: 'spike',
  page_title: 'Breaking story',
  spike_ratio: 5.2,
  severity: 'critical',
  timestamp: new Date().toISOString(),
  edits_5min: 30,
};

const editWarAlert: EditWarAlert = {
  type: 'edit_war',
  page_title: 'Debate topic',
  editor_count: 4,
  edit_count: 18,
  revert_count: 9,
  severity: 'high',
  start_time: new Date().toISOString(),
};

beforeEach(() => {
  MockWebSocket.instances = [];
  vi.stubGlobal('WebSocket', MockWebSocket);
  mockedGetAlerts.mockResolvedValue([spikeAlert, editWarAlert]);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('AlertsPanel', () => {
  it('renders header with title', async () => {
    render(<AlertsPanel />);
    expect(screen.getByText(/Breaking News Alerts/)).toBeInTheDocument();
  });

  it('shows alerts from initial REST fetch', async () => {
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('Breaking story')).toBeInTheDocument();
    });

    expect(screen.getByText('Debate topic')).toBeInTheDocument();
  });

  it('shows alert count badge', async () => {
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('2')).toBeInTheDocument();
    });
  });

  it('shows connection status', async () => {
    render(<AlertsPanel />);
    // Should mention connecting or live
    await waitFor(() => {
      const liveEl = screen.queryByText('Live');
      const connectingEl = screen.queryByText('Connecting');
      expect(liveEl || connectingEl).not.toBeNull();
    });
  });

  it('has sound toggle button', () => {
    render(<AlertsPanel />);
    expect(
      screen.getByLabelText(/alert sounds/)
    ).toBeInTheDocument();
  });

  it('renders severity filter buttons', () => {
    render(<AlertsPanel />);
    expect(screen.getByText('Critical')).toBeInTheDocument();
    expect(screen.getByText('High')).toBeInTheDocument();
    expect(screen.getByText('Medium')).toBeInTheDocument();
    expect(screen.getByText('Low')).toBeInTheDocument();
  });

  it('renders type filter buttons', () => {
    render(<AlertsPanel />);
    expect(screen.getByText('Spike')).toBeInTheDocument();
    expect(screen.getByText('Edit War')).toBeInTheDocument();
  });

  it('filters by severity', async () => {
    const user = userEvent.setup();
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('Breaking story')).toBeInTheDocument();
    });

    // Filter to high only — should hide critical spike
    await user.click(screen.getByText('High'));

    expect(screen.queryByText('Breaking story')).not.toBeInTheDocument();
    expect(screen.getByText('Debate topic')).toBeInTheDocument();
  });

  it('filters by type', async () => {
    const user = userEvent.setup();
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('Breaking story')).toBeInTheDocument();
    });

    // Filter to edit_war only — use getAllByText since "Edit War" also appears in the alert card header
    const editWarButtons = screen.getAllByText('Edit War');
    // The filter button is the smaller pill button, find it among the matches
    const filterButton = editWarButtons.find(
      (el) => el.className.includes('rounded-full'),
    );
    expect(filterButton).toBeDefined();
    await user.click(filterButton!);

    expect(screen.queryByText('Breaking story')).not.toBeInTheDocument();
    expect(screen.getByText('Debate topic')).toBeInTheDocument();
  });

  it('shows empty filter message when filters exclude all', async () => {
    const user = userEvent.setup();
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('Breaking story')).toBeInTheDocument();
    });

    // Filter to low severity — neither alert matches
    await user.click(screen.getByText('Low'));

    expect(screen.getByText(/No alerts match/)).toBeInTheDocument();
  });

  it('shows empty state when no alerts exist', async () => {
    mockedGetAlerts.mockResolvedValue([]);
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText(/No alerts yet/)).toBeInTheDocument();
    });
  });

  it('dismiss button removes individual alert', async () => {
    const user = userEvent.setup();
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('Breaking story')).toBeInTheDocument();
    });

    const dismissButtons = screen.getAllByLabelText('Dismiss alert');
    await user.click(dismissButtons[0]);

    expect(screen.queryByText('Breaking story')).not.toBeInTheDocument();
    expect(screen.getByText('Debate topic')).toBeInTheDocument();
  });

  it('clear all button removes all alerts', async () => {
    const user = userEvent.setup();
    render(<AlertsPanel />);

    await waitFor(() => {
      expect(screen.getByText('Breaking story')).toBeInTheDocument();
    });

    await user.click(screen.getByLabelText('Clear all alerts'));

    expect(screen.queryByText('Breaking story')).not.toBeInTheDocument();
    expect(screen.queryByText('Debate topic')).not.toBeInTheDocument();
    expect(screen.getByText(/No alerts yet/)).toBeInTheDocument();
  });
});
