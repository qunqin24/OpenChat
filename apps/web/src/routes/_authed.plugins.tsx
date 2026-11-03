import { createFileRoute } from '@tanstack/react-router'

import PluginsPage from '@/components/chat/PluginsPage'

/** Plugins to install, beside the chat in the same shell. */
export const Route = createFileRoute('/_authed/plugins')({
  component: PluginsPage,
})
