import type { Edit } from '../types';

/**
 * Convert an array of Edit results to CSV format.
 */
export function editsToCsv(edits: Edit[]): string {
  const headers = ['Title', 'User', 'Wiki', 'Bot', 'Comment', 'Byte Change', 'Timestamp'];
  const rows = edits.map((edit) => {
    const byteChange =
      edit.byte_change !== undefined
        ? edit.byte_change
        : (edit.length?.new ?? 0) - (edit.length?.old ?? 0);
    const ts =
      typeof edit.timestamp === 'number'
        ? new Date(edit.timestamp * 1000).toISOString()
        : edit.timestamp;
    return [
      csvEscape(edit.title),
      csvEscape(edit.user),
      csvEscape(edit.wiki),
      edit.bot ? 'Yes' : 'No',
      csvEscape(edit.comment),
      String(byteChange),
      ts,
    ].join(',');
  });

  return [headers.join(','), ...rows].join('\n');
}

function csvEscape(value: string): string {
  if (!value) return '""';
  const escaped = value.replace(/"/g, '""');
  return `"${escaped}"`;
}

/**
 * Trigger a file download from a string blob.
 */
export function downloadCsv(csvContent: string, filename: string): void {
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

/**
 * Export search results as a CSV file.
 */
export function exportSearchResults(edits: Edit[]): void {
  const csv = editsToCsv(edits);
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
  downloadCsv(csv, `search-results-${timestamp}.csv`);
}
