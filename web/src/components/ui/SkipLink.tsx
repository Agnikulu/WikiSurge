/**
 * Skip-to-content link for keyboard navigation accessibility.
 * Must be the first focusable element in the page.
 */
export function SkipLink() {
  return (
    <a
      href="#main-content"
      className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[100] focus:px-4 focus:py-2 focus:rounded-md focus:text-sm focus:font-medium focus:outline-none focus:ring-2"
      style={{ fontFamily: 'monospace' }}
    >
      Skip to main content
    </a>
  );
}
