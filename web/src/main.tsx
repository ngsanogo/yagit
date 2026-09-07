import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';

import './design/index.css';
import { App } from './App';

const rootElement = document.getElementById('root');
if (rootElement === null) {
  throw new Error('#root element not found: index.html was not served correctly');
}

window.addEventListener('unhandledrejection', (event) => {
  console.error('Unhandled promise rejection in yagit', event.reason);
});

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
