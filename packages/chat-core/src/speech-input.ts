import { isOpenChatError } from '@openchat/api-client'
import type { TFunction } from 'i18next'

// The composer's microphone (语音输入, docs/design/speech-input-plan.md): what
// a recording is recorded as, where its text goes in the box, and the words
// for what went wrong. Pure, so the web and the app read it the same way.

type T = TFunction<'common'>

/**
 * The recording formats, in the order to try them: Opus in WebM where the
 * browser has it (Chrome, Firefox, Edge), MP4 (AAC) on Safari. All of them are
 * formats the transcription service takes.
 */
export const RECORDING_TYPES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg;codecs=opus'] as const

/** The first recording type the browser can record, or undefined to let it choose. */
export function pickRecordingType(isSupported: (type: string) => boolean): string | undefined {
  return RECORDING_TYPES.find((type) => {
    try {
      return isSupported(type)
    } catch {
      return false
    }
  })
}

/** A recording shorter than this is a slip of the finger: nothing is sent. */
export const MIN_RECORDING_MS = 600

/** m:ss, for the timer beside the waveform. */
export function formatClock(seconds: number): string {
  const whole = Math.max(0, Math.floor(seconds))
  return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`
}

const WORD = /[\p{L}\p{N}]/u
const WIDE = /[\p{Script=Han}\p{Script=Hiragana}\p{Script=Katakana}\p{Script=Hangul}\u3000-\u303f\uff00-\uffef]/u

/**
 * The box's text with what was said put in place of the selection (or at the
 * caret), and where the caret goes after it. A space keeps it apart from a word
 * right beside it, as a typed word would be - but not between characters of a
 * script written without spaces (Chinese, Japanese).
 */
export function insertDictation(value: string, start: number, end: number, said: string): { value: string; caret: number } {
  const text = said.trim()
  const from = Math.max(0, Math.min(start, value.length))
  const to = Math.max(from, Math.min(end, value.length))
  if (!text) return { value, caret: to }
  const before = value.slice(0, from)
  const after = value.slice(to)
  const spaced = (left: string, right: string) =>
    Boolean(left) && Boolean(right) && WORD.test(left) && WORD.test(right) && !WIDE.test(left) && !WIDE.test(right)
  const lead = spaced(before.slice(-1), text[0]!) ? ' ' : ''
  const trail = spaced(text.slice(-1), after[0] ?? '') ? ' ' : ''
  const inserted = lead + text + trail
  return { value: before + inserted + after, caret: before.length + inserted.length }
}

/** Server error codes of POST /transcriptions that have their own wording. */
const CODES = [
  'speech_input_off',
  'audio_too_long',
  'audio_too_large',
  'unsupported_audio',
  'model_capacity_reached',
  'model_rate_limited',
] as const

/** What went wrong with a recording, in words. */
export type DictationProblem = 'denied' | 'no-microphone' | 'unsupported' | 'too-short' | 'empty' | 'failed'

const PROBLEMS: readonly DictationProblem[] = ['denied', 'no-microphone', 'unsupported', 'too-short', 'empty', 'failed']

/** The words for a recording that came to nothing: a problem of the browser's, or a failed transcription. */
export function dictationErrorText(t: T, problem: DictationProblem | unknown): string {
  const own = PROBLEMS.find((p) => p === problem)
  if (own) return t(`chat.voice.error.${own}`)
  const code = isOpenChatError(problem) ? CODES.find((c) => c === problem.code) : undefined
  return code ? t(`chat.voice.error.${code}`) : t('chat.voice.error.failed')
}
