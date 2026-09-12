import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import { dismissBootSplash, showBootSplash } from '@/lib/boot'

/** One cycle: splash in, hold, fade out, pause, repeat. */
const SPLASH_HOLD_MS = 1800
const SPLASH_GAP_MS = 900

/** How long each simulated request takes, to drive the top bar. */
const REQUEST_MS = 1400
const REQUEST_GAP_MS = 700

/**
 * Loops both loading states so they can be looked at.
 *
 * The top bar is driven by a real query rather than faked: it reads
 * `useIsFetching`, so the only honest way to preview it is to have something
 * actually in flight.
 */
export function LoadingPreview() {
  // The splash and the bar both stand still under this setting — deliberately,
  // but it looks identical to a broken animation, so it is surfaced here.
  const [reducedMotion, setReducedMotion] = useState(
    () => window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  )

  useEffect(() => {
    const media = window.matchMedia('(prefers-reduced-motion: reduce)')
    const onChange = () => setReducedMotion(media.matches)
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [])

  const [splashRunning, setSplashRunning] = useState(false)
  const [barRunning, setBarRunning] = useState(true)
  const [tick, setTick] = useState(0)

  // --- splash loop --------------------------------------------------------
  useEffect(() => {
    if (!splashRunning) return

    let cancelled = false
    let timer = 0

    const cycle = () => {
      if (cancelled) return
      showBootSplash()
      timer = window.setTimeout(() => {
        if (cancelled) return
        dismissBootSplash()
        timer = window.setTimeout(cycle, SPLASH_GAP_MS)
      }, SPLASH_HOLD_MS)
    }

    cycle()

    return () => {
      cancelled = true
      window.clearTimeout(timer)
      dismissBootSplash()
    }
  }, [splashRunning])

  // --- top bar loop -------------------------------------------------------
  // Each tick is a new query key, so the request genuinely re-runs and the bar
  // sees in-flight work.
  useQuery({
    queryKey: ['dev', 'loading-preview', tick],
    queryFn: () => new Promise((resolve) => window.setTimeout(() => resolve(tick), REQUEST_MS)),
    enabled: barRunning,
    gcTime: 0,
    staleTime: 0,
  })

  useEffect(() => {
    if (!barRunning) return
    const timer = window.setTimeout(() => setTick((n) => n + 1), REQUEST_MS + REQUEST_GAP_MS)
    return () => window.clearTimeout(timer)
  }, [barRunning, tick])

  return (
    <div className="n-auth">
      <div className="n-auth-card" style={{ maxWidth: 520 }}>
        <h1 className="n-auth-title">加载动画预览</h1>
        <p className="n-auth-sub">开发预览页，两种全局加载状态各自循环。</p>

        {reducedMotion && (
          <div className="n-auth-alert" role="status" style={{ marginBottom: 16 }}>
            你的系统开启了「减少动态效果」，所以两个动画都被刻意停用了 —— 看到的静止
            条不是 bug。要看动画，先在系统设置里关掉它。
          </div>
        )}

        <div className="n-auth-form">
          <button
            type="button"
            className="n-auth-submit"
            onClick={() => setSplashRunning((value) => !value)}
          >
            <span>{splashRunning ? '停止首屏遮罩' : '循环播放首屏遮罩'}</span>
          </button>

          <button
            type="button"
            className="n-auth-submit"
            onClick={() => setBarRunning((value) => !value)}
          >
            <span>{barRunning ? '停止顶部进度条' : '循环播放顶部进度条'}</span>
          </button>
        </div>

        <div className="n-auth-hint" style={{ textAlign: 'left' }}>
          <strong>顶部进度条</strong>：现在正在循环 —— 每 {REQUEST_MS / 1000}s
          一次请求，间隔 {REQUEST_GAP_MS / 1000}s。前 300ms 不显示，缓动到 90%
          等待，完成时才到 100%。
          <br />
          <br />
          <strong>首屏遮罩</strong>：默认不播，因为它是全屏的，播起来会盖住这个页面
          （每 {SPLASH_HOLD_MS / 1000}s 淡出一次，能看到间隙）。它平时只在
          index.html 里、React 挂载前出现一次。
        </div>
      </div>
    </div>
  )
}

export default LoadingPreview
