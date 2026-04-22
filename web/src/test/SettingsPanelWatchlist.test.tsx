import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, within, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('../store/authStore');
vi.mock('../components/Settings/AdminPanel', () => ({
  AdminPanel: () => <div data-testid="admin-panel" />,
}));

import { useAuthStore } from '../store/authStore';
const mockedUseAuthStore = vi.mocked(useAuthStore);

// ── Helpers ───────────────────────────────────────────────────────────────────

import type { User } from '../types/user';
import { SettingsPanel } from '../components/Settings/SettingsPanel';

function makeUser(overrides: Partial<User> = {}): User {
  return {
    id: 'user-abc-123',
    email: 'test@example.com',
    verified: true,
    is_admin: false,
    digest_frequency: 'daily',
    digest_content: 'both',
    spike_threshold: 3.0,
    watchlist: [],
    ...overrides,
  };
}

const updateWatchlistMock = vi.fn().mockResolvedValue(undefined);
const updatePreferencesMock = vi.fn().mockResolvedValue(undefined);
const clearErrorMock = vi.fn();
const logoutMock = vi.fn();

function setupStore(user: User | null = makeUser(), extra: Record<string, unknown> = {}) {
  mockedUseAuthStore.mockReturnValue({
    user,
    token: 'fake-token',
    isLoading: false,
    error: null,
    adminUsers: null,
    adminUsersLoading: false,
    updateWatchlist: updateWatchlistMock,
    updatePreferences: updatePreferencesMock,
    clearError: clearErrorMock,
    logout: logoutMock,
    login: vi.fn(),
    register: vi.fn(),
    fetchProfile: vi.fn(),
    fetchAdminUsers: vi.fn(),
    deleteAdminUser: vi.fn(),
    ...extra,
  });
}

/** Build a Wikipedia OpenSearch response */
function wikiResponse(titles: string[]) {
  return Promise.resolve({
    json: () => Promise.resolve(['query', titles, [], []]),
    ok: true,
  });
}

// ── Test setup ────────────────────────────────────────────────────────────────

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.clearAllMocks();
  global.fetch = vi.fn();
});

afterEach(() => {
  vi.useRealTimers();
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SettingsPanel – watchlist autocomplete', () => {
  // ── Rendering ──────────────────────────────────────────────────────────────

  it('renders the watchlist search input', () => {
    setupStore();
    render(<SettingsPanel />);
    expect(screen.getByPlaceholderText(/Search Wikipedia article title/i)).toBeInTheDocument();
  });

  it('renders empty-state message when watchlist is empty', () => {
    setupStore();
    render(<SettingsPanel />);
    expect(screen.getByText(/No pages in your watchlist yet/i)).toBeInTheDocument();
  });

  it('renders existing watchlist items from store', () => {
    setupStore(makeUser({ watchlist: ['Bitcoin', 'Taylor Swift'] }));
    render(<SettingsPanel />);
    expect(screen.getByText('Bitcoin')).toBeInTheDocument();
    expect(screen.getByText('Taylor Swift')).toBeInTheDocument();
  });

  it('shows 0/100 counter initially', () => {
    setupStore();
    render(<SettingsPanel />);
    expect(screen.getByText('0/100')).toBeInTheDocument();
  });

  // ── Debounce: no fetch for short input ────────────────────────────────────

  it('does NOT call fetch for a single character', async () => {
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'B');
    await act(async () => { vi.advanceTimersByTime(400); });

    expect(global.fetch).not.toHaveBeenCalled();
    expect(screen.queryByRole('listitem')).not.toBeInTheDocument();
  });

  it('does NOT call fetch for empty input', async () => {
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'Bi');
    await act(async () => { vi.advanceTimersByTime(400); });
    vi.mocked(global.fetch).mockClear();

    await user.clear(input);
    await act(async () => { vi.advanceTimersByTime(400); });

    expect(global.fetch).not.toHaveBeenCalled();
  });

  // ── Debounce: fires after 300ms ───────────────────────────────────────────

  it('calls the backend autocomplete API after 300ms debounce', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    // Before debounce fires
    expect(global.fetch).not.toHaveBeenCalled();
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(global.fetch).toHaveBeenCalledTimes(1));

    const url = vi.mocked(global.fetch).mock.calls[0][0] as string;
    expect(url).toContain('/api/wiki/autocomplete');
    expect(url).toContain('q=Bit');
    expect(url).toContain('lang=en');
  });

  it('resets the debounce timer on each keystroke', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'Bi');
    await act(async () => { vi.advanceTimersByTime(200); }); // not fired yet
    await user.type(input, 't');
    await act(async () => { vi.advanceTimersByTime(200); }); // still not fired
    expect(global.fetch).not.toHaveBeenCalled();

    await act(async () => { vi.advanceTimersByTime(200); }); // now total 400ms from last key
    await waitFor(() => expect(global.fetch).toHaveBeenCalledTimes(1));
  });

  // ── Dropdown visibility ───────────────────────────────────────────────────

  it('shows suggestion dropdown after fetch returns results', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash', 'Bitcoin SV']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    expect(screen.getByText('Bitcoin Cash')).toBeInTheDocument();
    expect(screen.getByText('Bitcoin SV')).toBeInTheDocument();
  });

  it('hides dropdown when fetch returns empty results', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse([]) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'xyznotarticle');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(global.fetch).toHaveBeenCalled());

    expect(screen.queryByRole('listitem')).not.toBeInTheDocument();
  });

  it('hides dropdown when input is cleared after results were shown', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    await user.clear(input);
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.queryByText('Bitcoin')).not.toBeInTheDocument());
  });

  it('closes dropdown on Escape key', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    await user.keyboard('{Escape}');
    expect(screen.queryByText('Bitcoin')).not.toBeInTheDocument();
  });

  it('closes dropdown on outside click', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <div>
        <SettingsPanel />
        <button data-testid="outside">outside</button>
      </div>
    );

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    await user.click(screen.getByTestId('outside'));
    await waitFor(() => expect(screen.queryByText('Bitcoin')).not.toBeInTheDocument());
  });

  it('re-shows dropdown on input focus when suggestions exist', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <div>
        <SettingsPanel />
        <button data-testid="outside">outside</button>
      </div>
    );

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    // close via outside click
    await user.click(screen.getByTestId('outside'));
    await waitFor(() => expect(screen.queryByText('Bitcoin')).not.toBeInTheDocument());

    // re-focus should show again
    await user.click(input);
    expect(screen.getByText('Bitcoin')).toBeInTheDocument();
  });

  // ── Error resilience ──────────────────────────────────────────────────────

  it('silently hides dropdown when Wikipedia fetch throws', async () => {
    vi.mocked(global.fetch).mockRejectedValue(new Error('Network error') as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(global.fetch).toHaveBeenCalled());

    expect(screen.queryByRole('listitem')).not.toBeInTheDocument();
  });

  // ── Adding items via click ────────────────────────────────────────────────

  it('adds a suggestion to the watchlist when clicked', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    await user.click(screen.getByText('Bitcoin'));

    // Should appear in the watchlist
    const list = screen.getByRole('list');
    expect(within(list).getByText('Bitcoin')).toBeInTheDocument();

    // Input cleared, dropdown gone
    expect(screen.getByPlaceholderText(/Search Wikipedia article title/i)).toHaveValue('');
    expect(screen.queryByText('Bitcoin Cash')).not.toBeInTheDocument();
  });

  it('increments the watchlist counter after adding', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());
    await user.click(screen.getByText('Bitcoin'));

    expect(screen.getByText('1/100')).toBeInTheDocument();
  });

  // ── Adding items via keyboard ─────────────────────────────────────────────

  it('adds active suggestion on Enter', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    await user.keyboard('{ArrowDown}{Enter}');

    const list = screen.getByRole('list');
    expect(within(list).getByText('Bitcoin')).toBeInTheDocument();
  });

  it('navigates suggestions with ArrowDown / ArrowUp', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash', 'Bitcoin SV']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    // ArrowDown twice → active = index 1
    await user.keyboard('{ArrowDown}{ArrowDown}');
    await user.keyboard('{Enter}');

    const list = screen.getByRole('list');
    expect(within(list).getByText('Bitcoin Cash')).toBeInTheDocument();
  });

  it('ArrowUp does not go below index -1', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    // Press ArrowUp when nothing selected — then Enter should use typed text
    await user.keyboard('{ArrowUp}{Enter}');

    const list = screen.getByRole('list');
    // "Bit" was typed, no active suggestion, so typed title added as-is
    expect(within(list).getByText('Bit')).toBeInTheDocument();
  });

  it('adds free-text title on Enter when no suggestion is active', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['OpenAI']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    await user.type(input, 'OpenAI');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('OpenAI')).toBeInTheDocument());

    // Don't navigate — just press Enter with no active item
    await user.keyboard('{Enter}');

    // First OpenAI in list will be the suggestion item; after Enter it should be in watchlist
    const listItems = screen.getAllByText('OpenAI');
    // At least one is in the watchlist ul
    const list = screen.getByRole('list');
    expect(within(list).getByText('OpenAI')).toBeInTheDocument();
  });

  // ── Duplicate prevention ──────────────────────────────────────────────────

  it('prevents adding a duplicate title', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore(makeUser({ watchlist: ['Bitcoin'] }));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bitcoin');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => {
      // The suggestion with "already added" label
      expect(screen.getByText('already added')).toBeInTheDocument();
    });

    // Click the suggestion — should be a no-op
    const suggestionItems = screen.getAllByText('Bitcoin');
    await user.click(suggestionItems[0]);

    // Still only one Bitcoin in the watchlist
    expect(screen.getAllByText('Bitcoin')).toHaveLength(2); // one in list, one in dropdown label area
    expect(screen.getByText('1/100')).toBeInTheDocument();
  });

  it('shows "already added" label for watchlisted suggestions', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin', 'Bitcoin Cash']) as never);
    setupStore(makeUser({ watchlist: ['Bitcoin'] }));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('already added')).toBeInTheDocument());

    // Bitcoin Cash does NOT have the label
    expect(screen.queryAllByText('already added')).toHaveLength(1);
  });

  // ── Max 100 items ─────────────────────────────────────────────────────────

  it('disables the add button when watchlist is full (100 items)', () => {
    const fullWatchlist = Array.from({ length: 100 }, (_, i) => `Page_${i}`);
    setupStore(makeUser({ watchlist: fullWatchlist }));
    render(<SettingsPanel />);

    // Add button should be disabled
    // It targets the button next to the input that contains Plus/Loader2 icon
    const input = screen.getByPlaceholderText(/Search Wikipedia article title/i);
    // Button is the sibling button in the same flex row
    const addButton = input.closest('div')!.querySelector('button') as HTMLButtonElement;
    expect(addButton).toBeDisabled();
  });

  it('does not add via click when watchlist is at 100 items', async () => {
    const fullWatchlist = Array.from({ length: 100 }, (_, i) => `Page_${i}`);
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore(makeUser({ watchlist: fullWatchlist }));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());

    await user.click(screen.getByText('Bitcoin'));
    expect(screen.getByText('100/100')).toBeInTheDocument();
  });

  // ── Remove items ──────────────────────────────────────────────────────────

  it('removes a page from the watchlist on trash click', async () => {
    setupStore(makeUser({ watchlist: ['Bitcoin', 'OpenAI'] }));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    const listItem = screen.getByText('Bitcoin').closest('li')!;
    const trashButton = within(listItem).getByTitle('Remove from watchlist');
    await user.click(trashButton);

    expect(screen.queryByText('Bitcoin')).not.toBeInTheDocument();
    expect(screen.getByText('OpenAI')).toBeInTheDocument();
    expect(screen.getByText('1/100')).toBeInTheDocument();
  });

  // ── Save ──────────────────────────────────────────────────────────────────

  it('calls updateWatchlist with correct titles on save', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Bitcoin']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Bit');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(screen.getByText('Bitcoin')).toBeInTheDocument());
    await user.click(screen.getByText('Bitcoin'));

    await user.click(screen.getByText('SAVE WATCHLIST'));
    await waitFor(() => expect(updateWatchlistMock).toHaveBeenCalledWith(['Bitcoin']));
  });

  it('shows SAVED state briefly after successful save', async () => {
    setupStore(makeUser({ watchlist: ['Bitcoin'] }));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.click(screen.getByText('SAVE WATCHLIST'));
    await waitFor(() => expect(screen.getByText('SAVED')).toBeInTheDocument());

    act(() => { vi.advanceTimersByTime(2500); });
    await waitFor(() => expect(screen.getByText('SAVE WATCHLIST')).toBeInTheDocument());
  });

  // ── Wikipedia URL construction ────────────────────────────────────────────

  it('URL-encodes the search query in the backend autocomplete call', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['Taylor Swift']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'Taylor Swift');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(global.fetch).toHaveBeenCalled());

    const url = vi.mocked(global.fetch).mock.calls[0][0] as string;
    expect(url).toContain('/api/wiki/autocomplete');
    expect(url).toContain('q=Taylor%20Swift');
    expect(url).toContain('lang=en');
  });

  it('uses the backend autocomplete endpoint', async () => {
    vi.mocked(global.fetch).mockReturnValue(wikiResponse(['A', 'B', 'C', 'D', 'E', 'F', 'G']) as never);
    setupStore();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SettingsPanel />);

    await user.type(screen.getByPlaceholderText(/Search Wikipedia article title/i), 'test');
    await act(async () => { vi.advanceTimersByTime(300); });
    await waitFor(() => expect(global.fetch).toHaveBeenCalled());

    const url = vi.mocked(global.fetch).mock.calls[0][0] as string;
    expect(url).toContain('/api/wiki/autocomplete');
  });

  // ── Null / loading guard ──────────────────────────────────────────────────

  it('renders nothing when user is null', () => {
    setupStore(null);
    const { container } = render(<SettingsPanel />);
    expect(container.firstChild).toBeNull();
  });

  it('renders sign-in fallback for malformed user (missing id)', () => {
    setupStore({ email: 'test@example.com' } as User);
    render(<SettingsPanel />);
    expect(screen.getByText(/Session data is incomplete/i)).toBeInTheDocument();
  });
});
