import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SearchInterface } from '../components/Search/SearchInterface';

// Mock the API
vi.mock('../utils/api', () => ({
  searchEdits: vi.fn(),
}));

import { searchEdits } from '../utils/api';

const mockSearchEdits = vi.mocked(searchEdits);

describe('SearchInterface', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  it('renders the search interface heading', () => {
    render(<SearchInterface />);
    expect(screen.getByText('Search Edits')).toBeInTheDocument();
  });

  it('renders the search input', () => {
    render(<SearchInterface />);
    expect(screen.getByPlaceholderText('Search pages, users, or comments...')).toBeInTheDocument();
  });

  it('renders advanced search button', () => {
    render(<SearchInterface />);
    expect(screen.getByText('Advanced search')).toBeInTheDocument();
  });

  it('shows initial empty state', () => {
    render(<SearchInterface />);
    expect(screen.getByText('Enter a search query to find edits')).toBeInTheDocument();
  });

  it('performs search on Enter key', async () => {
    const user = userEvent.setup();
    mockSearchEdits.mockResolvedValueOnce({
      edits: [
        {
          id: '1',
          title: 'Test Page',
          user: 'User1',
          wiki: 'enwiki',
          bot: false,
          timestamp: '2026-01-15T12:00:00Z',
          comment: 'Test comment',
          byte_change: 100,
        },
      ],
      total: 1,
    });

    render(<SearchInterface />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    await user.type(input, 'test{Enter}');

    // Wait for async render
    expect(mockSearchEdits).toHaveBeenCalled();
  });

  it('shows error on API failure', async () => {
    const user = userEvent.setup();
    mockSearchEdits.mockRejectedValueOnce(new Error('Network error'));

    render(<SearchInterface />);
    const input = screen.getByPlaceholderText('Search pages, users, or comments...');
    await user.type(input, 'fail{Enter}');

    // Wait for error to appear
    const errorEl = await screen.findByText('Search failed. Please try again.');
    expect(errorEl).toBeInTheDocument();
  });

  it('opens advanced search modal', async () => {
    const user = userEvent.setup();
    render(<SearchInterface />);
    await user.click(screen.getByText('Advanced search'));
    expect(screen.getByText('Advanced Search')).toBeInTheDocument();
  });

  it('closes advanced search modal', async () => {
    const user = userEvent.setup();
    render(<SearchInterface />);
    await user.click(screen.getByText('Advanced search'));
    expect(screen.getByText('Advanced Search')).toBeInTheDocument();
    await user.click(screen.getByLabelText('Close advanced search'));
    // Modal heading should be gone
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
