import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Plus, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useCreateConversation } from '@/hooks/use-conversations'

export const Route = createFileRoute('/')({
  component: HomeRoute,
})

function HomeRoute() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const createConversation = useCreateConversation()

  const handleStart = async () => {
    const conversation = await createConversation.mutateAsync({})
    await navigate({ to: '/chat/$conversationId', params: { conversationId: conversation.id } })
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-6 px-6 text-center">
      <Sparkles className="size-8 text-primary" />
      <div className="space-y-2">
        <h1 className="text-2xl font-semibold">{t('chat.empty.title')}</h1>
        <p className="max-w-md text-sm text-muted-foreground">{t('chat.empty.description')}</p>
      </div>
      <Button
        variant="primary"
        size="lg"
        onClick={() => void handleStart()}
        disabled={createConversation.isPending}
      >
        <Plus />
        {t('nav.newChat')}
      </Button>
    </div>
  )
}
