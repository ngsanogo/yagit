import js from '@eslint/js';
import globals from 'globals';
import typescriptEslint from 'typescript-eslint';
import reactHooks from 'eslint-plugin-react-hooks';

export default typescriptEslint.config(
  { ignores: ['dist', 'node_modules', 'test-results', 'playwright-report'] },
  js.configs.recommended,
  ...typescriptEslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: { globals: globals.browser },
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // The TypeScript counterpart of the "never write _ = err" rule on the
      // Go side: an ignored value has to be ignored on purpose, under a name
      // that says so.
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    },
  },
  {
    // The project's tooling runs under Node, not in the browser, so it uses
    // process, console and the node: modules. Without this block no-undef
    // flags every one of them — an avalanche of false positives, and a linter
    // people learn to ignore is no longer worth anything.
    files: ['scripts/**/*.{js,mjs}', 'e2e/**/*.ts', '*.config.{js,ts}'],
    languageOptions: { globals: globals.node },
  },
);
