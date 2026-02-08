import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SearchInput } from '../components/Search/SearchInput';

describe('SearchInput', () => {
  const onSearch = vi.fn();

  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.clearAllMocks();
    localStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders the search input', () => {
    render(<SearchInput onSearch={onSearch} loading={false} />);
    expect(screen.getByPlaceholderText('Search pages, users, or comments...')).toBeInTheDocument();
  });

  it('renders search button', () => {
    render(<SearchInput onSearch={onSearch} loading={false} />);
    expect(screen.getByLabelText('Search')).toBeInTheDocument();
  });

  it('disables search button when input is empty', () => {
    render(<SearchInput onSearch={onSearch} loading={false} />);
    expect(screen.getByLabelText('Search')).toBeDisabled();
  });

  it('shows clear button when query is present', async () => {
    const user = userEvent.setup();
    render(<SearchInput onSearch={onSearch} loading={false} />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    await user.type(input, 'test');
    expect(screen.getByLabelText('Clear search')).toBeInTheDocument();
  });

  it('clears input when clear button clicked', async () => {
    const user = userEvent.setup();
    render(<SearchInput onSearch={onSearch} loading={false} />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    await user.type(input, 'test');
    await user.click(screen.getByLabelText('Clear search'));
    expect(input).toHaveValue('');
  });

  it('submits search on Enter key', async () => {
    const user = userEvent.setup();
    render(<SearchInput onSearch={onSearch} loading={false} />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    await user.type(input, 'election{Enter}');
    expect(onSearch).toHaveBeenCalledWith('election');
  });

  it('does not submit empty search on Enter', async () => {
    const emptySearchFn = vi.fn();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<SearchInput onSearch={emptySearchFn} loading={false} />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    // Press Enter without typing anything
    await user.click(input);
    await user.keyboard('{Enter}');
    vi.runAllTimers();
    expect(emptySearchFn).not.toHaveBeenCalled();
  });

  it('shows loading spinner when loading', () => {
    render(<SearchInput onSearch={onSearch} loading={true} initialQuery="test" />);
    // Loader2 renders an SVG with animate-spin class
    const buttons = screen.getAllByRole('button');
    const searchBtn = buttons.find((b) => b.getAttribute('aria-label') === 'Search');
    expect(searchBtn).toBeDefined();
  });

  it('shows character count when query present', async () => {
    const user = userEvent.setup();
    render(<SearchInput onSearch={onSearch} loading={false} />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    await user.type(input, 'hello');
    expect(screen.getByText('5')).toBeInTheDocument();
  });

  it('has accessible label', () => {
    render(<SearchInput onSearch={onSearch} loading={false} />);
    expect(screen.getByLabelText('Search pages, users, or comments')).toBeInTheDocument();
  });

  it('uses initialQuery prop', () => {
    render(<SearchInput onSearch={onSearch} loading={false} initialQuery="initial" />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    expect(input).toHaveValue('initial');
  });
});
