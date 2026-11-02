import { createFileRoute } from '@tanstack/react-router'

import { SiteSkills } from '@/features/admin/skills'

export const Route = createFileRoute('/admin/skills')({
  component: SiteSkills,
})
