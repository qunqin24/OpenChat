import { useCallback, useEffect, useRef, useState } from 'react'

import { api, unwrap } from '@/lib/api'
import { reportError } from '@/lib/query'
import { MIN_RECORDING_MS, pickRecordingType, type DictationProblem } from '@openchat/chat-core/speech-input'

/**
 * The composer's microphone (语音输入, docs/design/speech-input-plan.md): it
 * records, shows how loud and how long, and on `finish` sends the recording to
 * be transcribed and hands the text back. Nothing is kept: the recording is
 * thrown away once the text is back, or at `cancel`.
 */

export type DictationState = 'idle' | 'starting' | 'recording' | 'transcribing'

/** How many loudness samples the waveform shows, newest last. */
export const WAVE_SAMPLES = 72

/** Whether this browser can record at all (and the page may ask for the microphone). */
export function canDictate(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.isSecureContext &&
    typeof MediaRecorder !== 'undefined' &&
    typeof navigator.mediaDevices?.getUserMedia === 'function'
  )
}

export function useDictation({
  maxSeconds,
  onText,
  onProblem,
}: {
  maxSeconds: number
  /** What was said, once it is back. */
  onText: (text: string) => void
  /** Why a recording came to nothing: the browser's problem or the transcription's error. */
  onProblem: (problem: DictationProblem | unknown) => void
}) {
  const [state, setState] = useState<DictationState>('idle')
  const [seconds, setSeconds] = useState(0)
  const [levels, setLevels] = useState<number[]>(() => Array(WAVE_SAMPLES).fill(0))

  const session = useRef<{
    stream: MediaStream
    recorder: MediaRecorder
    audio: AudioContext
    chunks: Blob[]
    started: number
    frame: number
    timer: number
    keep: boolean
  } | null>(null)
  const request = useRef<AbortController | null>(null)
  // The latest callbacks, for a recording that outlives the render it began in.
  const handlers = useRef({ onText, onProblem })
  useEffect(() => {
    handlers.current = { onText, onProblem }
  })

  /** Lets go of the microphone and the audio graph; the recording, if kept, is sent by the recorder's stop. */
  const release = useCallback(() => {
    const s = session.current
    if (!s) return
    cancelAnimationFrame(s.frame)
    window.clearInterval(s.timer)
    for (const track of s.stream.getTracks()) track.stop()
    void s.audio.close().catch(() => {})
  }, [])

  const transcribe = useCallback(async (blob: Blob, elapsedMs: number) => {
    if (elapsedMs < MIN_RECORDING_MS || blob.size === 0) {
      handlers.current.onProblem('too-short')
      setState('idle')
      return
    }
    const controller = new AbortController()
    request.current = controller
    setState('transcribing')
    try {
      const result = unwrap(
        await api.POST('/transcriptions', {
          params: { query: { seconds: Math.max(1, Math.round(elapsedMs / 1000)) } },
          // The recording itself is the body, sent as it is.
          body: blob as unknown as string,
          bodySerializer: (body: unknown) => body as BodyInit,
          headers: { 'Content-Type': blob.type || 'audio/webm' },
          signal: controller.signal,
        }),
      )
      const text = result.text.trim()
      if (text) handlers.current.onText(text)
      else handlers.current.onProblem('empty')
    } catch (error) {
      if (!controller.signal.aborted) {
        // A session that ended or an account disabled is noticed here too.
        reportError(error)
        handlers.current.onProblem(error)
      }
    } finally {
      if (request.current === controller) request.current = null
      setState('idle')
    }
  }, [])

  const start = useCallback(async () => {
    if (session.current || state !== 'idle') return
    setState('starting')
    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (error) {
      const name = error instanceof DOMException ? error.name : ''
      handlers.current.onProblem(name === 'NotAllowedError' || name === 'SecurityError' ? 'denied' : 'no-microphone')
      setState('idle')
      return
    }
    const type = pickRecordingType((t) => MediaRecorder.isTypeSupported(t))
    let recorder: MediaRecorder
    try {
      recorder = new MediaRecorder(stream, type ? { mimeType: type } : undefined)
    } catch {
      for (const track of stream.getTracks()) track.stop()
      handlers.current.onProblem('unsupported')
      setState('idle')
      return
    }

    // Loudness for the waveform: the root mean square of each frame.
    const audio = new AudioContext()
    const analyser = audio.createAnalyser()
    analyser.fftSize = 1024
    audio.createMediaStreamSource(stream).connect(analyser)
    const frame = new Uint8Array(analyser.fftSize)

    const s = { stream, recorder, audio, chunks: [] as Blob[], started: performance.now(), frame: 0, timer: 0, keep: false }
    session.current = s
    recorder.ondataavailable = (event) => {
      if (event.data.size > 0) s.chunks.push(event.data)
    }
    recorder.onstop = () => {
      const elapsed = performance.now() - s.started
      release()
      session.current = null
      setLevels(Array(WAVE_SAMPLES).fill(0))
      if (!s.keep) {
        setState('idle')
        return
      }
      void transcribe(new Blob(s.chunks, { type: recorder.mimeType || type || 'audio/webm' }), elapsed)
    }

    let last = 0
    const sample = (now: number) => {
      // About twenty samples a second: a bar per 50ms scrolls at a readable pace.
      if (now - last >= 50) {
        last = now
        analyser.getByteTimeDomainData(frame)
        let sum = 0
        for (const v of frame) sum += ((v - 128) / 128) ** 2
        const level = Math.min(1, Math.sqrt(sum / frame.length) * 4)
        setLevels((previous) => [...previous.slice(1), level])
      }
      s.frame = requestAnimationFrame(sample)
    }
    s.frame = requestAnimationFrame(sample)
    s.timer = window.setInterval(() => {
      const elapsed = (performance.now() - s.started) / 1000
      setSeconds(elapsed)
      // At the longest a recording may be, it is finished for the person.
      if (elapsed >= maxSeconds && recorder.state === 'recording') {
        s.keep = true
        recorder.stop()
      }
    }, 200)

    setSeconds(0)
    recorder.start(1000)
    setState('recording')
  }, [maxSeconds, release, state, transcribe])

  /** Stops recording and sends what was recorded. */
  const finish = useCallback(() => {
    const s = session.current
    if (!s || s.recorder.state !== 'recording') return
    s.keep = true
    s.recorder.stop()
  }, [])

  /** Throws the recording away, or stops waiting for its text. */
  const cancel = useCallback(() => {
    const s = session.current
    if (s && s.recorder.state === 'recording') {
      s.keep = false
      s.recorder.stop()
      return
    }
    request.current?.abort()
  }, [])

  // Leaving the page (or the composer going away) lets go of the microphone.
  useEffect(
    () => () => {
      const s = session.current
      if (s) {
        s.keep = false
        if (s.recorder.state === 'recording') s.recorder.stop()
        release()
      }
      request.current?.abort()
    },
    [release],
  )

  return { state, seconds, levels, start, finish, cancel }
}
