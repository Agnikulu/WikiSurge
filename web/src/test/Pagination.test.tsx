import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Pagination } from '../components/Search/Pagination';

describe('Pagination', () => {
  it('renders nothing when totalPages is 1', () => {
    const { container } = render(
      <Pagination currentPage={1} totalPages={1} onPageChange={() => {}} />
    );
    expect(container.innerHTML).toBe('');
  });

  it('renders page info text', () => {
    render(<Pagination currentPage={2} totalPages={5} onPageChange={() => {}} />);
    expect(screen.getByText('Page 2 of 5')).toBeInTheDocument();
  });

  it('disables previous button on page 1', () => {
    render(<Pagination currentPage={1} totalPages={5} onPageChange={() => {}} />);
    const prev = screen.getByLabelText('Previous page');
    expect(prev).toBeDisabled();
  });

  it('disables next button on last page', () => {
    render(<Pagination currentPage={5} totalPages={5} onPageChange={() => {}} />);
    const next = screen.getByLabelText('Next page');
    expect(next).toBeDisabled();
  });

  it('enables previous and next on middle page', () => {
    render(<Pagination currentPage={3} totalPages={5} onPageChange={() => {}} />);
    expect(screen.getByLabelText('Previous page')).not.toBeDisabled();
    expect(screen.getByLabelText('Next page')).not.toBeDisabled();
  });

  it('highlights current page', () => {
    render(<Pagination currentPage={3} totalPages={5} onPageChange={() => {}} />);
    const currentBtn = screen.getByRole('button', { name: '3' });
    expect(currentBtn).toHaveAttribute('aria-current', 'page');
  });

  it('calls onPageChange when clicking a page number', async () => {
    const onPageChange = vi.fn();
    render(<Pagination currentPage={1} totalPages={5} onPageChange={onPageChange} />);
    const page3 = screen.getByRole('button', { name: '3' });
    await page3.click();
    expect(onPageChange).toHaveBeenCalledWith(3);
  });

  it('calls onPageChange with previous page', async () => {
    const onPageChange = vi.fn();
    render(<Pagination currentPage={3} totalPages={5} onPageChange={onPageChange} />);
    await screen.getByLabelText('Previous page').click();
    expect(onPageChange).toHaveBeenCalledWith(2);
  });

  it('calls onPageChange with next page', async () => {
    const onPageChange = vi.fn();
    render(<Pagination currentPage={3} totalPages={5} onPageChange={onPageChange} />);
    await screen.getByLabelText('Next page').click();
    expect(onPageChange).toHaveBeenCalledWith(4);
  });

  it('shows all pages when total <= 7', () => {
    render(<Pagination currentPage={1} totalPages={6} onPageChange={() => {}} />);
    for (let i = 1; i <= 6; i++) {
      expect(screen.getByRole('button', { name: String(i) })).toBeInTheDocument();
    }
  });

  it('shows ellipsis for many pages', () => {
    render(<Pagination currentPage={5} totalPages={20} onPageChange={() => {}} />);
    const ellipses = screen.getAllByText('…');
    expect(ellipses.length).toBeGreaterThanOrEqual(1);
  });
});
