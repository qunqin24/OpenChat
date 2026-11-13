import { OpenChatError } from '@openchat/api-client'
import { describe, expect, it } from 'vitest'

import { dictationErrorText, formatClock, insertDictation, pickRecordingType } from './speech-input'

const t = ((key: string) => key) as never

describe('speech input', () => {
  it('records in the first format the browser has', () => {
    expect(pickRecordingType((type) => type.startsWith('audio/mp4'))).toBe('audio/mp4')
    expect(pickRecordingType(() => true)).toBe('audio/webm;codecs=opus')
    expect(pickRecordingType(() => false)).toBeUndefined()
    // A browser whose check throws has none of them.
    expect(pickRecordingType(() => { throw new Error('no') })).toBeUndefined()
  })

  it('shows the time as m:ss', () => {
    expect([0, 9.9, 61, 600].map(formatClock)).toEqual(['0:00', '0:09', '1:01', '10:00'])
  })

  it('puts what was said at the caret, spaced from a word beside it', () => {
    expect(insertDictation('', 0, 0, ' 你好 ')).toEqual({ value: '你好', caret: 2 })
    expect(insertDictation('帮我翻译：', 5, 5, '今天天气不错')).toEqual({ value: '帮我翻译：今天天气不错', caret: 11 })
    expect(insertDictation('send it to', 10, 10, 'Alice')).toEqual({ value: 'send it to Alice', caret: 16 })
    expect(insertDictation('hello world', 6, 6, 'big')).toEqual({ value: 'hello big world', caret: 10 })
    // Chinese beside Latin needs no space: the words do not run together.
    expect(insertDictation('用Go写', 1, 1, '一个')).toEqual({ value: '用一个Go写', caret: 3 })
    // A selection is replaced.
    expect(insertDictation('a bad idea', 2, 5, 'good')).toEqual({ value: 'a good idea', caret: 6 })
    // A caret past the end (the text changed meanwhile) is the end.
    expect(insertDictation('ok', 9, 9, 'then')).toEqual({ value: 'ok then', caret: 7 })
    // Nothing said changes nothing.
    expect(insertDictation('ok', 1, 1, '  ')).toEqual({ value: 'ok', caret: 1 })
  })

  it('words what went wrong', () => {
    expect(dictationErrorText(t, 'denied')).toBe('chat.voice.error.denied')
    const tooLong = new OpenChatError(413, { code: 'audio_too_long', message: 'x' })
    expect(dictationErrorText(t, tooLong)).toBe('chat.voice.error.audio_too_long')
    expect(dictationErrorText(t, new OpenChatError(500, { code: 'internal', message: 'x' }))).toBe('chat.voice.error.failed')
    expect(dictationErrorText(t, new Error('network'))).toBe('chat.voice.error.failed')
  })
})
