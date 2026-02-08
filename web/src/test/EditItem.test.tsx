import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EditItem } from '../components/LiveFeed/EditItem';
import type { Edit } from '../types';

const now = Math.floor(Date.now() / 1000);

const baseEdit: Edit = {
  id: 123456,
  type: 'edit',
  title: 'Climate change',
  user: 'JohnDoe',
  wiki: 'enwiki',
  bot: false,
  server_url: 'https://en.wikipedia.org',
  timestamp: now - 5,
  length: { old: 5000, new: 5200 },
  revision: { old: 100, new: 101 },
  comment: 'Added references',
};

describe('EditItem', () => {
  it('renders page title', () => {
    render(<EditItem edit={baseEdit} />);
    expect(screen.getByText('Climate change')).toBeInTheDocument();
  });

  it('renders user name', () => {
    render(<EditItem edit={baseEdit} />);
    expect(screen.getByText('JohnDoe')).toBeInTheDocument();
  });

  it('renders language indicator', () => {
    render(<EditItem edit={baseEdit} />);
    expect(screen.getByText('en')).toBeInTheDocument();
  });

  it('renders positive byte change in green', () => {
    render(<EditItem edit={baseEdit} />);
    const byteEl = screen.getByText('+200');
    expect(byteEl).toBeInTheDocument();
    expect(byteEl.className).toContain('text-green-600');
  });

  it('renders negative byte change in red', () => {
    const edit: Edit = {
      ...baseEdit,
      length: { old: 5000, new: 4500 },
    };
    render(<EditItem edit={edit} />);
    const byteEl = screen.getByText('-500');
    expect(byteEl).toBeInTheDocument();
    expect(byteEl.className).toContain('text-red-600');
  });

  it('shows bot indicator for bot edits', () => {
    const edit: Edit = { ...baseEdit, bot: true, user: 'CleanupBot' };
    render(<EditItem edit={edit} />);
    expect(screen.getByText('bot')).toBeInTheDocument();
  });

  it('does not show bot indicator for non-bot edits', () => {
    render(<EditItem edit={baseEdit} />);
    expect(screen.queryByText('bot')).not.toBeInTheDocument();
  });

  it('shows new page indicator for new pages', () => {
    const edit: Edit = {
      ...baseEdit,
      length: { old: 0, new: 3000 },
    };
    render(<EditItem edit={edit} />);
    expect(screen.getByText('new')).toBeInTheDocument();
  });

  it('shows large edit indicator for edits >1000 bytes', () => {
    const edit: Edit = {
      ...baseEdit,
      length: { old: 1000, new: 3000 },
    };
    render(<EditItem edit={edit} />);
    expect(screen.getByText('large')).toBeInTheDocument();
  });

  it('truncates long titles', () => {
    const edit: Edit = {
      ...baseEdit,
      title: 'A'.repeat(100),
    };
    render(<EditItem edit={edit} />);
    // Title truncated to 45 chars + ellipsis
    const heading = screen.getByRole('heading', { level: 3 });
    expect(heading.textContent!.length).toBeLessThan(100);
  });

  it('renders comment text', () => {
    render(<EditItem edit={baseEdit} />);
    expect(screen.getByText('Added references')).toBeInTheDocument();
  });

  it('renders relative timestamp', () => {
    render(<EditItem edit={baseEdit} />);
    // Timing may vary slightly during test execution
    expect(screen.getByText(/\d+s ago/)).toBeInTheDocument();
  });

  it('has proper ARIA label', () => {
    render(<EditItem edit={baseEdit} />);
    const item = screen.getByRole('button');
    expect(item).toHaveAttribute('aria-label');
    expect(item.getAttribute('aria-label')).toContain('Climate change');
  });

  it('calls onClick when clicked', async () => {
    const user = userEvent.setup();
    let clicked: Edit | null = null;
    render(<EditItem edit={baseEdit} onClick={(e) => (clicked = e)} />);

    await user.click(screen.getByRole('button'));
    expect(clicked).not.toBeNull();
    expect(clicked!.title).toBe('Climate change');
  });

  it('is keyboard accessible', async () => {
    const user = userEvent.setup();
    let clicked = false;
    render(<EditItem edit={baseEdit} onClick={() => (clicked = true)} />);

    const item = screen.getByRole('button');
    item.focus();
    await user.keyboard('{Enter}');
    expect(clicked).toBe(true);
  });

  it('applies muted styling for bot edits', () => {
    const botEdit: Edit = { ...baseEdit, bot: true };
    const { container } = render(<EditItem edit={botEdit} />);
    const article = container.querySelector('article');
    expect(article?.className).toContain('bg-gray-50');
  });

  it('handles search API shape (string id, byte_change)', () => {
    const searchEdit: Edit = {
      id: 'abc123',
      title: 'Test Page',
      user: 'TestUser',
      wiki: 'enwiki',
      bot: false,
      timestamp: new Date().toISOString(),
      byte_change: 500,
      comment: 'Test edit',
    };
    render(<EditItem edit={searchEdit} />);
    expect(screen.getByText('Test Page')).toBeInTheDocument();
    expect(screen.getByText('+500')).toBeInTheDocument();
  });
});
