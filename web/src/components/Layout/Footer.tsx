export function Footer() {
  return (
    <footer className="bg-white border-t border-gray-200 mt-auto">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
        <div className="flex items-center justify-between text-sm text-gray-500">
          <p>WikiSurge &mdash; Real-time Wikipedia Edit Analytics</p>
          <p>
            Data from{' '}
            <a
              href="https://stream.wikimedia.org/"
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary-600 hover:text-primary-700 underline"
            >
              Wikimedia EventStreams
            </a>
          </p>
        </div>
      </div>
    </footer>
  );
}
