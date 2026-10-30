import type { ChatModel } from '@openchat/api-client'

/**
 * The code a model at its spending cap is refused with: on sending, and on the
 * error row of a run stopped between two requests. The words for it are
 * `chat.send-error.*` and `chat.error.*`.
 */
export const CAPACITY_CODE = 'model_capacity_reached'

/**
 * The model a composer starts on when nothing was chosen for the conversation:
 * the one last used if it is offered and has room, else the first that has
 * room, else the first offered (every model is at its cap, and the composer
 * says so when something is sent). Never silently swaps a model the person
 * picked - only this fallback, which nobody chose, steers around a cap.
 */
export function defaultModelId(models: readonly ChatModel[], remembered?: string): string {
  const open = (model: ChatModel) => !model.atCapacity
  const last = remembered ? models.find((model) => model.id === remembered) : undefined
  return (last && open(last) ? last : (models.find(open) ?? models[0]))?.id ?? ''
}
