import { createFileRoute } from '@tanstack/react-router'

import { Authentication } from '@/features/admin/authentication'

export const Route = createFileRoute('/admin/authentication')({
  component: Authentication,
})
