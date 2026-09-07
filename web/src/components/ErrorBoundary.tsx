import { Component, type ErrorInfo, type ReactNode } from 'react';

import { Button } from './Button';
import { EmptyState } from './EmptyState';

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
}

/**
 * The last line of defence against a blank screen.
 *
 * React Query catches failed requests; this catches failed renders — a shape
 * the daemon did not promise, a null where the code assumed a value. Without
 * it the root unmounts and the user sees nothing.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error('yagit hit an unexpected render error', error, info.componentStack);
  }

  private reload = (): void => {
    window.location.reload();
  };

  private tryAgain = (): void => {
    this.setState({ error: null });
  };

  override render(): ReactNode {
    const { error } = this.state;
    if (error === null) {
      return this.props.children;
    }

    return (
      <div className="grid min-h-dvh place-items-center bg-canvas p-8">
        <EmptyState
          title="Something in the interface broke"
          description={error.message}
          action={
            <div className="flex gap-2">
              <Button variant="primary" onClick={this.tryAgain}>
                Try again
              </Button>
              <Button onClick={this.reload}>Reload</Button>
            </div>
          }
        />
      </div>
    );
  }
}
