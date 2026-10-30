import type { ToolCallPart, ToolResultPart } from '@openchat/api-client'

/** The tool the model asks the user questions with. */
export const ASK_USER = 'ask_user'

export interface AskOption {
  label: string
  description?: string
}

export interface AskQuestion {
  question: string
  header: string
  options: AskOption[]
  multiSelect: boolean
}

/** What the user chose for one question, and what they wrote beside or instead. */
export interface AskAnswer {
  selected: string[]
  other?: string
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

/**
 * The questions of a call, or null while its arguments are still streaming or
 * when they are not a question the server would have put (it answers those
 * with an error, and the model asks again).
 */
export function parseQuestions(args: ToolCallPart['arguments']): AskQuestion[] | null {
  const list = args?.questions
  if (!Array.isArray(list) || list.length === 0) return null
  const questions: AskQuestion[] = []
  for (const item of list) {
    if (!isRecord(item) || typeof item.question !== 'string' || !Array.isArray(item.options)) return null
    const options: AskOption[] = []
    for (const option of item.options) {
      if (!isRecord(option) || typeof option.label !== 'string') return null
      options.push({
        label: option.label,
        ...(typeof option.description === 'string' && option.description ? { description: option.description } : {}),
      })
    }
    questions.push({
      question: item.question,
      header: typeof item.header === 'string' ? item.header : '',
      options,
      multiSelect: item.multiSelect === true,
    })
  }
  return questions
}

/** The answers recorded with a call's result, by question position. */
export function answersOf(result: ToolResultPart | undefined): AskAnswer[] | null {
  if (result?.status !== 'succeeded') return null
  const list = result.details?.answers
  if (!Array.isArray(list)) return null
  return list.map((item) => {
    const answer = isRecord(item) ? item : {}
    return {
      selected: Array.isArray(answer.selected) ? answer.selected.filter((s): s is string => typeof s === 'string') : [],
      ...(typeof answer.other === 'string' && answer.other ? { other: answer.other } : {}),
    }
  })
}
