import type { GuestHub } from './hub'
import { GuestError } from './errors'
import type { GuestRepo } from './repo'

/**
 * Taking a guest's compression back. Compressing itself is only ever part of a
 * run (`GuestRunner` keeps the summary the run's events and its last frame
 * bring); nobody can ask for it, so the page has nothing else to do here.
 */
export interface CompactionDeps {
  repo: GuestRepo
  hub: GuestHub
  /** The conversation's summary changed: other pages showing it are told. */
  changed?: (conversationId: string) => void
}

const busy = () => new GuestError(409, 'conversation_busy', 'this conversation is already answering')

/** Takes the compression back: the model is sent the whole conversation again. */
export async function undoGuestCompaction(deps: CompactionDeps, conversationId: string): Promise<void> {
  const { repo } = deps
  const data = await repo.data(conversationId)
  if (repo.activeRun(data)) throw busy()
  if (!(await repo.summaryOf(data))) return
  await repo.setSummary(conversationId, undefined)
  deps.hub.announceCompaction(conversationId, undefined)
  deps.changed?.(conversationId)
}
