import js from '@eslint/js'
import tseslint from 'typescript-eslint'

// What runs in both the browser and React Native has neither's globals and
// no React of its own (a second React in the app is the classic hooks bug):
// a module that needs one stays in its app.
export default tseslint.config(
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.ts'],
    rules: {
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      'no-restricted-globals': [
        'error',
        ...['window', 'document', 'localStorage', 'sessionStorage', 'navigator', 'location', 'BroadcastChannel', 'matchMedia'].map(
          (name) => ({ name, message: 'chat-core runs on the web and on React Native: keep platform globals in the app.' }),
        ),
      ],
      'no-restricted-imports': [
        'error',
        {
          paths: ['react', 'react-dom', 'react-native', '@tanstack/react-router', 'react-i18next'].map((name) => ({
            name,
            message: 'chat-core has no React: keep components and hooks in the app.',
          })),
          patterns: [{ group: ['@tanstack/react-query'], message: 'Use @tanstack/query-core types.', allowTypeImports: true }],
        },
      ],
    },
  },
)
