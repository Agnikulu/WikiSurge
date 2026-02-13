import { Heart, Github, BookOpen, ExternalLink } from 'lucide-react';

const techBadges = [
  { label: 'React', color: 'rgba(0,255,136,0.6)' },
  { label: 'Go', color: 'rgba(0,221,255,0.6)' },
  { label: 'Kafka', color: 'rgba(255,170,0,0.6)' },
  { label: 'Elasticsearch', color: 'rgba(255,255,0,0.5)' },
  { label: 'Redis', color: 'rgba(255,68,68,0.6)' },
];

export function Footer() {
  return (
    <footer className="mt-auto" style={{ background: '#0d1525', borderTop: '1px solid rgba(0,255,136,0.1)' }} role="contentinfo">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-5">
        {/* Tech badges */}
        <div className="flex flex-wrap items-center justify-center gap-2 mb-4">
          {techBadges.map((b) => (
            <span
              key={b.label}
              className="inline-flex items-center px-2.5 py-0.5 rounded-full text-[10px] font-mono font-medium"
              style={{
                color: b.color,
                background: `${b.color}15`,
                border: `1px solid ${b.color}30`,
              }}
            >
              {b.label.toUpperCase()}
            </span>
          ))}
        </div>

        <div className="flex flex-col sm:flex-row items-center justify-between gap-3 text-[11px] font-mono" style={{ color: 'rgba(0,255,136,0.3)' }}>
          {/* Left: attribution */}
          <p className="flex items-center gap-1">
            Made with <Heart className="h-3 w-3" style={{ color: '#ff4444', fill: '#ff4444' }} aria-hidden="true" /> by{' '}
            <a
              href="https://agnikbanerjee.com"
              target="_blank"
              rel="noopener noreferrer"
              className="transition-colors"
              style={{ color: 'rgba(0,255,136,0.5)' }}
              onMouseEnter={(e) => (e.currentTarget.style.color = '#00ff88')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'rgba(0,255,136,0.5)')}
            >
              Agnik Banerjee
            </a>
          </p>

          {/* Center: copyright */}
          <p>&copy; {new Date().getFullYear()} WikiSurge &mdash; Real-time Wikipedia Intelligence</p>

          {/* Right: links */}
          <div className="flex items-center space-x-4">
            <a
              href="https://github.com"
              target="_blank"
              rel="noopener noreferrer"
              className="transition-colors flex items-center gap-1"
              style={{ color: 'rgba(0,255,136,0.3)' }}
              onMouseEnter={(e) => (e.currentTarget.style.color = '#00ff88')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'rgba(0,255,136,0.3)')}
              aria-label="GitHub repository"
            >
              <Github className="h-3.5 w-3.5" aria-hidden="true" />
              <span>GitHub</span>
            </a>
            <a
              href="/docs/API.md"
              target="_blank"
              rel="noopener noreferrer"
              className="transition-colors flex items-center gap-1"
              style={{ color: 'rgba(0,255,136,0.3)' }}
              onMouseEnter={(e) => (e.currentTarget.style.color = '#00ff88')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'rgba(0,255,136,0.3)')}
              aria-label="API documentation"
            >
              <BookOpen className="h-3.5 w-3.5" aria-hidden="true" />
              <span>API Docs</span>
            </a>
            <a
              href="https://stream.wikimedia.org/"
              target="_blank"
              rel="noopener noreferrer"
              className="transition-colors flex items-center gap-1"
              style={{ color: 'rgba(0,255,136,0.3)' }}
              onMouseEnter={(e) => (e.currentTarget.style.color = '#00ff88')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'rgba(0,255,136,0.3)')}
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
