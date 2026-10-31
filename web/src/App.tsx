import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { lazy, Suspense, useState } from 'react';

import { Workbench } from './app/Workbench';
import { ErrorBoundary } from './components/ErrorBoundary';
import { Spinner } from './components/Spinner';
import { ToastHost } from './components/ToastHost';

// The showcase is a reference page, not a workbench screen. Loading it with
// the product would put its whole catalogue in the bundle a repository opens.
const Showcase = lazy(() =>
  import('./design/Showcase').then((module) => ({ default: module.Showcase })),
);

/**
 * Application root.
 *
 * Two screens, selected by the path: `/` is the workbench, `/design` is the
 * showcase. the no-router choice chose no router library; until there are more URLs worth
 * bookmarking, reading the pathname is the honest amount of machinery.
 *
 * The showcase is not linked from the workbench. It stays reachable at
 * `/design` so the e2e suite — and anyone who wants the reference — can open
 * every component state without a control on the product surface.
 */
export function App() {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            // yagit reads a repository the user is also working in from their
            // terminal, so what is on screen goes stale for reasons this
            // application never sees. The event stream of the one-event-stream design announces
            // what touches the git directory; the work tree has no watch and
            // is polled instead, only while the window has focus.
            // Refetching on focus is what covers the gap that leaves — a file
            // saved while this tab was in the background.
            refetchOnWindowFocus: true,
            staleTime: 5_000,
            // A failed git command is not a network blip. Retrying it three
            // times only delays showing the user what git actually said.
            retry: false,
          },
        },
      }),
  );

  const showingDesignSystem = window.location.pathname === '/design';

  return (
    <ErrorBoundary>
      <QueryClientProvider client={client}>
        <ToastHost>
          {showingDesignSystem ? (
            <Suspense
              fallback={
                <div className="flex min-h-dvh items-center justify-center">
                  <Spinner label="Loading the design system" />
                </div>
              }
            >
              <Showcase />
            </Suspense>
          ) : (
            <Workbench />
          )}
        </ToastHost>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
