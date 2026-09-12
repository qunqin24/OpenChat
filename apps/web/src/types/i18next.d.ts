import 'i18next'

import type { resources } from '@/lib/i18n'

// Makes t() keys autocomplete and typo-proof.
declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common'
    resources: (typeof resources)['en']
  }
}
