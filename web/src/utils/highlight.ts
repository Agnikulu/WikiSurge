/**
 * Search term highlighting utility.
 * Splits text into segments with match metadata for safe React rendering.
 */

export interface HighlightSegment {
  text: string;
  match: boolean;
}

/**
 * Split text into segments, marking substrings that match the query.
 * Case-insensitive matching.
 */
export function highlightMatches(text: string, query: string): HighlightSegment[] {
  if (!query.trim() || !text) {
    return [{ text, match: false }];
  }

  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const regex = new RegExp(`(${escaped})`, 'gi');
  const parts = text.split(regex);

  return parts
    .filter((part) => part.length > 0)
    .map((part) => ({
      text: part,
      match: regex.test(part) || part.toLowerCase() === query.toLowerCase(),
    }));
}
