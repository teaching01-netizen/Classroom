import React from 'react';

export class ErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error) {
    return { hasError: true, error };
  }

  componentDidCatch(error, errorInfo) {
    console.error('ErrorBoundary caught:', error, errorInfo);
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null });
  };

  render() {
    if (this.state.hasError) {
      return (
        <div style={{
          padding: '32px',
          textAlign: 'center',
          minHeight: '50vh',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: '16px',
        }}>
          <h2 style={{ fontSize: '1.5rem', fontWeight: '600', color: 'var(--color-danger, #ef4444)' }}>
            Something went wrong
          </h2>
          <p style={{ color: 'var(--text-secondary, #94a3b8)', maxWidth: '480px' }}>
            {this.state.error?.message || 'An unexpected error occurred.'}
          </p>
          <button
            onClick={this.handleRetry}
            style={{
              padding: '10px 24px',
              borderRadius: 'var(--radius-md, 8px)',
              border: 'none',
              background: 'var(--color-accent, #6366f1)',
              color: '#fff',
              fontWeight: '500',
              cursor: 'pointer',
            }}
          >
            Try Again
          </button>
        </div>
      );
    }

    return this.props.children;
  }
}
