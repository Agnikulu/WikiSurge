import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { EditWar } from '../types';
import * as apiModule from '../utils/api';
import { EditWarsList } from '../components/EditWars/EditWarsList';

// ── Mocks ──────────────────────────────────────────────

const mockWars: EditWar[] = [
  {
    page_title: 'Active War Page',
    editors: ['Alice', 'Bob'],
    edit_count: 10,
    revert_count: 4,
    severity: 'high',
    start_time: new Date(Date.now() - 20 * 60_000).toISOString(),
    last_edit: new Date().toISOString(),
    active: true,
  },
  {
    page_title: 'Critical War Page',
    editors: ['Charlie', 'Dave', 'Eve'],
    edit_count: 25,
    revert_count: 15,
    severity: 'critical',
    start_time: new Date(Date.now() - 45 * 60_000).toISOString(),
    last_edit: new Date().toISOString(),
    active: true,
  },
];

vi.mock('../utils/api', () => ({
  getEditWars: vi.fn(() => Promise.resolve(mockWars)),
}));

vi.mock('../utils/notifications', () => ({
  requestNotificationPermission: vi.fn(),
  showEditWarNotification: vi.fn(),
}));

vi.mock('../utils/alertSounds', () => ({
  playEditWarAlert: vi.fn(),
}));

describe('EditWarsList', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    // Reset mock to default (return mockWars)
    vi.mocked(apiModule.getEditWars).mockImplementation(() => Promise.resolve(mockWars));
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it('renders the header', async () => {
    render(<EditWarsList />);
    await waitFor(() => {
      expect(screen.getByText(/EDIT WARS IN PROGRESS/)).toBeInTheDocument();
    });
  });

  it('shows loading skeleton initially', () => {
    const { container } = render(<EditWarsList />);
    expect(container.querySelector('.animate-pulse')).not.toBeNull();
  });

  it('displays edit wars after loading', async () => {
    render(<EditWarsList />);
    await waitFor(() => {
      expect(screen.getByText('Active War Page')).toBeInTheDocument();
      expect(screen.getByText('Critical War Page')).toBeInTheDocument();
    });
  });

  it('sorts wars by severity (critical first)', async () => {
    render(<EditWarsList />);
    await waitFor(() => {
      const titles = screen
        .getAllByRole('link')
        .filter((el) =>
          ['Active War Page', 'Critical War Page'].includes(
            el.textContent ?? '',
          ),
        );
      // Critical should appear first
      expect(titles[0].textContent).toBe('Critical War Page');
    });
  });

  it('shows active count badge', async () => {
    render(<EditWarsList />);
    await waitFor(() => {
      expect(screen.getByText('2')).toBeInTheDocument();
    });
  });

  it('shows filter toggle buttons', async () => {
    render(<EditWarsList />);
    await waitFor(() => {
      expect(screen.getByText('ACTIVE')).toBeInTheDocument();
      expect(screen.getByText('ALL')).toBeInTheDocument();
    });
  });

  it('collapses and expands on header click', async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<EditWarsList />);

    await waitFor(() => {
      expect(screen.getByText('Active War Page')).toBeInTheDocument();
    });

    // Click the header toggle to collapse
    const header = screen.getByText(/EDIT WARS IN PROGRESS/);
    await user.click(header);

    // Cards should be hidden
    expect(screen.queryByText('Active War Page')).not.toBeInTheDocument();
  });

  it('shows empty state when no wars', async () => {
    vi.mocked(apiModule.getEditWars).mockResolvedValue([]);

    render(<EditWarsList />);

    await waitFor(() => {
      expect(
        screen.getByText('NO ACTIVE EDIT WARS DETECTED'),
      ).toBeInTheDocument();
    });
  });
});
