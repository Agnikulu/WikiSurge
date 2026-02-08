import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { EditWarCard } from '../components/EditWars/EditWarCard';
import type { EditWar } from '../types';

const now = new Date();
const thirtyMinsAgo = new Date(now.getTime() - 30 * 60_000);

const activeWar: EditWar = {
  page_title: 'Controversial Article',
  editors: ['AliceEditor', 'BobEditor', 'CharlieEditor'],
  edit_count: 12,
  revert_count: 5,
  severity: 'high',
  start_time: thirtyMinsAgo.toISOString(),
  last_edit: now.toISOString(),
  active: true,
};

const resolvedWar: EditWar = {
  ...activeWar,
  page_title: 'Resolved Dispute',
  severity: 'medium',
  active: false,
};

describe('EditWarCard', () => {
  it('renders the page title prominently', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('Controversial Article')).toBeInTheDocument();
  });

  it('shows "Active" status badge for active wars', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('Active')).toBeInTheDocument();
  });

  it('shows "Resolved" status badge for resolved wars', () => {
    render(<EditWarCard war={resolvedWar} />);
    const resolved = screen.getAllByText('Resolved');
    expect(resolved.length).toBeGreaterThanOrEqual(1);
  });

  it('displays severity badge', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('high')).toBeInTheDocument();
  });

  it('shows editor count', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('3 editors')).toBeInTheDocument();
  });

  it('shows edit count', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('12 edits')).toBeInTheDocument();
  });

  it('shows revert count', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('5 reverts')).toBeInTheDocument();
  });

  it('displays editor names', () => {
    render(<EditWarCard war={activeWar} />);
    expect(screen.getByText('AliceEditor')).toBeInTheDocument();
    expect(screen.getByText('BobEditor')).toBeInTheDocument();
    expect(screen.getByText('CharlieEditor')).toBeInTheDocument();
  });

  it('renders View page link pointing to Wikipedia', () => {
    render(<EditWarCard war={activeWar} />);
    const link = screen.getByText('View page');
    expect(link.closest('a')).toHaveAttribute(
      'href',
      expect.stringContaining('wikipedia.org'),
    );
  });

  it('renders Edit history link', () => {
    render(<EditWarCard war={activeWar} />);
    const link = screen.getByText('Edit history');
    expect(link.closest('a')).toHaveAttribute(
      'href',
      expect.stringContaining('action=history'),
    );
  });

  it('expands details on "Details" button click', async () => {
    const user = userEvent.setup();
    render(<EditWarCard war={activeWar} />);

    // Initially expanded details not visible
    expect(screen.queryByText('Revert ratio:')).not.toBeInTheDocument();

    // Click Details
    await user.click(screen.getByText('Details'));

    // Now visible
    expect(screen.getByText('Revert ratio:')).toBeInTheDocument();
    expect(screen.getByText('All editors:')).toBeInTheDocument();
  });

  it('collapses on "Less" button click', async () => {
    const user = userEvent.setup();
    render(<EditWarCard war={activeWar} />);

    await user.click(screen.getByText('Details'));
    expect(screen.getByText('Revert ratio:')).toBeInTheDocument();

    await user.click(screen.getByText('Less'));
    expect(screen.queryByText('Revert ratio:')).not.toBeInTheDocument();
  });

  it('calls onDismiss when dismiss button clicked', async () => {
    const onDismiss = vi.fn();
    const user = userEvent.setup();
    render(<EditWarCard war={activeWar} onDismiss={onDismiss} />);

    const dismissBtn = screen.getByLabelText('Dismiss edit war');
    await user.click(dismissBtn);
    expect(onDismiss).toHaveBeenCalledWith(activeWar);
  });

  it('applies orange-themed border for high severity', () => {
    const { container } = render(<EditWarCard war={activeWar} />);
    const card = container.firstElementChild;
    expect(card?.className).toContain('border-l-orange');
  });

  it('applies highlight ring when isNew is true', () => {
    const { container } = render(<EditWarCard war={activeWar} isNew />);
    const card = container.firstElementChild;
    expect(card?.className).toContain('ring-2');
  });

  it('truncates long titles with ellipsis', () => {
    const longTitle =
      'This is a very long article title that should be truncated by the component';
    const war: EditWar = { ...activeWar, page_title: longTitle };
    render(<EditWarCard war={war} />);
    const link = screen.getByTitle(longTitle);
    expect(link.textContent?.endsWith('…')).toBe(true);
  });

  it('shows +N more when there are more than 6 editors', () => {
    const manyEditors = Array.from({ length: 8 }, (_, i) => `Editor${i + 1}`);
    const war: EditWar = { ...activeWar, editors: manyEditors };
    render(<EditWarCard war={war} />);
    expect(screen.getByText('+2 more')).toBeInTheDocument();
  });

  it('renders severity badge with correct text', () => {
    const criticalWar: EditWar = { ...activeWar, severity: 'critical' };
    render(<EditWarCard war={criticalWar} />);
    expect(screen.getByText('critical')).toBeInTheDocument();
  });
});
