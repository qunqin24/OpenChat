import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import tseslint from 'typescript-eslint'

export default tseslint.config(
  { ignores: ['android', 'ios', '.expo', 'expo-env.d.ts', 'src/vendor/*.gen.js'] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  // Build scripts run in Node.
  { files: ['scripts/**/*.mjs'], languageOptions: { globals: globals.node } },
  {
    files: ['**/*.{ts,tsx}'],
    plugins: { 'react-hooks': reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  },
)
