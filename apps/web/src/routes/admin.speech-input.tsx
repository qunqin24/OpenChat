import { createFileRoute } from '@tanstack/react-router'

import { SpeechInputPage } from '@/features/admin/speech-input'

export const Route = createFileRoute('/admin/speech-input')({
  component: SpeechInputPage,
})
