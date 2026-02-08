import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { SearchResults } from '../components/Search/SearchResults';
import type { Edit } from '../types';

const mockResults: Edit[] = [
  {
    id: '1',
    title: 'Election results 2024',
    user: 'Editor1',
    wiki: 'enwiki',
    bot: false,
    timestamp: '2026-01-15T12:00:00Z',
    comment: 'Updated election results',
    byte_change: 150,
  },
  {
    id: '2',
    title: 'Second article',
    user: 'BotUser',
    wiki: 'dewiki',
    bot: true,
    timestamp: '2026-01-14T10:00:00Z',
    comment: 'Auto fix',
    byte_change: -20,
  },
];

const defaultProps = {
  results: mockResults,
  total: 2,
  loading: false,
  error: null,
  searched: true,
  query: 'election',
  currentPage: 1,
  totalPages: 1,
  sortBy: 'relevance' as const,
  onPageChange: vi.fn(),
  onSortChange: vi.fn(),
  perPage: 50,
};

describe('SearchResults', () => {
  it('shows loading skeletons when loading', () => {
    const { container } = render(
      <SearchResults {...defaultProps} loading={true} results={[]} />
    );
    const skeletons = container.querySelectorAll('.animate-pulse');
    expect(skeletons.length).toBeGreaterThan(0);
  });

  it('shows error state', () => {
    render(
      <SearchResults {...defaultProps} error="Network error" results={[]} />
    );
    expect(screen.getByText('Search failed. Please try again.')).toBeInTheDocument();
    expect(screen.getByText('Network error')).toBeInTheDocument();
  });

  it('shows initial state before search', () => {
    render(
      <SearchResults {...defaultProps} searched={false} results={[]} />
    );
    expect(screen.getByText('Enter a search query to find edits')).toBeInTheDocument();
  });

  it('shows no results message', () => {
    render(
      <SearchResults {...defaultProps} results={[]} total={0} />
    );
    expect(screen.getByText(/No edits found for/)).toBeInTheDocument();
  });

  it('includes query in no-results message', () => {
    render(
      <SearchResults {...defaultProps} results={[]} total={0} query="nonexistent" />
    );
    expect(screen.getByText('nonexistent')).toBeInTheDocument();
  });

  it('displays result count', () => {
    render(<SearchResults {...defaultProps} />);
    expect(screen.getByText(/Showing/)).toBeInTheDocument();
    expect(screen.getByText('1–2')).toBeInTheDocument();
  });

  it('renders result items', () => {
    render(<SearchResults {...defaultProps} />);
    expect(screen.getByText(/Editor1/)).toBeInTheDocument();
  });

  it('renders sort selector', () => {
    render(<SearchResults {...defaultProps} />);
    expect(screen.getByLabelText('Sort results')).toBeInTheDocument();
  });

  it('renders export button', () => {
    render(<SearchResults {...defaultProps} />);
    expect(screen.getByLabelText('Export results as CSV')).toBeInTheDocument();
  });

  it('highlights query in result titles', () => {
    render(<SearchResults {...defaultProps} />);
    // The word "election" should be wrapped in a <mark> tag
    const marks = document.querySelectorAll('mark');
    expect(marks.length).toBeGreaterThan(0);
  });
});
