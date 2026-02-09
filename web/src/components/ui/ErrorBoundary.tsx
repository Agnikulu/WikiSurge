import { Component, type ErrorInfo, type ReactNode } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';

interface ErrorBoundaryProps {
  children: ReactNode;
  /** Optional fallback to render instead of the default */
  fallback?: ReactNode;
  /** Error handler callback */
  onError?: (error: Error, errorInfo: ErrorInfo) => void;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error('[ErrorBoundary] Caught error:', error);
    console.error('[ErrorBoundary] Component stack:', errorInfo.componentStack);
    console.error('[ErrorBoundary] Error stack:', error.stack);
    this.props.onError?.(error, errorInfo);
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps) {
    if (this.state.hasError && prevProps.children !== this.props.children) {
      this.setState({ hasError: false, error: null });
    }
  }

  handleReload = () => {
    this.setState({ hasError: false, error: null });
  };

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <div
          role="alert"
          className="flex flex-col items-center justify-center p-8 my-4 rounded-lg text-center"
          style={{ background: '#111b2e', border: '1px solid rgba(255,68,68,0.3)' }}
        >
          <AlertTriangle className="h-12 w-12 mb-4" style={{ color: '#ff4444' }} aria-hidden="true" />
          <h2 className="text-lg font-semibold mb-2" style={{ color: '#ff4444', fontFamily: 'monospace', letterSpacing: '0.05em' }}>SYSTEM ERROR</h2>
          <p className="text-sm mb-4 max-w-md" style={{ color: 'rgba(0,255,136,0.5)', fontFamily: 'monospace' }}>
            An unexpected error occurred while rendering this section. Please try reloading.
          </p>
          <button
            onClick={this.handleReload}
            className="inline-flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium focus:outline-none focus:ring-2 transition-colors"
            style={{ background: 'rgba(0,255,136,0.15)', color: '#00ff88', border: '1px solid rgba(0,255,136,0.3)', fontFamily: 'monospace' }}
          >
            <RefreshCw className="h-4 w-4" aria-hidden="true" />
            RETRY
          </button>
        </div>
      );
    }

    return this.props.children;
  }
}
