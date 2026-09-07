import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useState } from 'react';

import { Workbench } from './app/Workbench';
import { ErrorBoundary } from './components/ErrorBoundary';
import { ToastHost } from './components/ToastHost';
import { Showcase } from './design/Showcase';

/**
 * Application root.
 *
 * Two screens, selected by the path: `/` is the workbench, `/design` is the
 * showcase. ADR 0006 chose no router library; until there are more URLs worth
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
            // application never sees. The event stream of ADR 0007 announces
            // what touches the git directory; the work tree has no watch and
            // is polled instead, only while the window has focus (ADR 0015).
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
        <ToastHost>{showingDesignSystem ? <Showcase /> : <Workbench />}</ToastHost>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
