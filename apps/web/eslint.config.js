import js from '@eslint/js'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['dist', 'src/routeTree.gen.ts'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': [
        'warn',
        { allowConstantExport: true, allowExportNames: ['buttonVariants'] },
      ],
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    },
  },
  {
    // Vendored from shadcn-admin and kept close to upstream so it can be
    // re-synced; their mixed exports are not ours to reorganise.
    files: ['src/components/shadcn/**'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
  {
    // Route modules must export `Route`, and main.tsx exports nothing at all.
    files: ['src/routes/**/*.tsx', 'src/main.tsx'],
    rules: { 'react-refresh/only-export-components': 'off' },
  },
)
