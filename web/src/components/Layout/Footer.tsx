import { Heart, Github, BookOpen, ExternalLink } from 'lucide-react';

const techBadges = [
  { label: 'React', color: 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300' },
  { label: 'Go', color: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300' },
  { label: 'Kafka', color: 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300' },
  { label: 'Elasticsearch', color: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300' },
  { label: 'Redis', color: 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300' },
];

export function Footer() {
  return (
    <footer className="bg-white dark:bg-gray-900 border-t border-gray-200 dark:border-gray-700 mt-auto" role="contentinfo">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-6">
        {/* Tech badges */}
        <div className="flex flex-wrap items-center justify-center gap-2 mb-4">
          {techBadges.map((b) => (
            <span key={b.label} className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${b.color}`}>
              {b.label}
            </span>
          ))}
        </div>

        <div className="flex flex-col sm:flex-row items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
          {/* Left: attribution */}
          <p className="flex items-center gap-1">
            Made with <Heart className="h-3 w-3 text-red-500 fill-red-500" aria-hidden="true" /> by WikiSurge
          </p>

          {/* Center: copyright */}
          <p>&copy; {new Date().getFullYear()} WikiSurge &mdash; Real-time Wikipedia Intelligence</p>

          {/* Right: links */}
          <div className="flex items-center space-x-4">
            <a
              href="https://github.com"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-gray-700 dark:hover:text-gray-200 transition-colors flex items-center gap-1"
              aria-label="GitHub repository"
            >
              <Github className="h-3.5 w-3.5" aria-hidden="true" />
              <span>GitHub</span>
            </a>
            <a
              href="/docs/API.md"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-gray-700 dark:hover:text-gray-200 transition-colors flex items-center gap-1"
              aria-label="API documentation"
            >
              <BookOpen className="h-3.5 w-3.5" aria-hidden="true" />
              <span>API Docs</span>
            </a>
            <a
              href="https://stream.wikimedia.org/"
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-gray-700 dark:hover:text-gray-200 transition-colors flex items-center gap-1"
              aria-label="Wikimedia EventStreams"
            >
              <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
              <span>EventStreams</span>
            </a>
          </div>
        </div>
      </div>
    </footer>
  );
}
