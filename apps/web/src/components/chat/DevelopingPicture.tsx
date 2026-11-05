import { useEffect, useRef } from 'react'

// The placeholder of a picture being made: 「显影 · 对焦」, chosen from the
// mockups (docs/design/image-generation-plan.md §10). A slow, domain-warped
// colour field comes into focus all at once - detail and colour together, a
// slight zoom settling - holds, and goes soft again; each round it comes into
// focus in another palette. One canvas and one WebGL program per placeholder,
// drawn only while it is on screen and the page is visible. Without WebGL the
// card's own background (a soft gradient in CSS) stands in.

const VERTEX = 'attribute vec2 p; void main(){ gl_Position = vec4(p, 0., 1.); }'

const FRAGMENT = `
precision highp float;
uniform vec2 uRes; uniform float uTime; uniform float uP; uniform float uDark;
uniform vec3 uA; uniform vec3 uB; uniform vec3 uC; uniform vec3 uD; uniform vec3 uE;
float hash(vec2 p){ p = fract(p*vec2(123.34, 456.21)); p += dot(p, p+45.32); return fract(p.x*p.y); }
float noise(vec2 p){ vec2 i=floor(p), f=fract(p); vec2 u=f*f*(3.-2.*f);
  return mix(mix(hash(i),hash(i+vec2(1,0)),u.x), mix(hash(i+vec2(0,1)),hash(i+vec2(1,1)),u.x), u.y); }
float fbm(vec2 p, float detail){ float v=0., a=.5; mat2 m=mat2(1.6,1.2,-1.2,1.6);
  for(int i=0;i<6;i++){ float w = i<2 ? 1. : detail; v += a*w*noise(p); p = m*p; a *= .5; } return v; }
void main(){
  vec2 uv = gl_FragCoord.xy / uRes; uv.y = 1. - uv.y;
  float zoom = 1.06 - .06 * uP;
  vec2 p = ((uv - .5) * zoom) * vec2(uRes.x/uRes.y, 1.) * 2.6;
  float detail = mix(.06, 1., uP);
  float t = uTime * .035;
  vec2 q = vec2(fbm(p + vec2(0., t), detail), fbm(p + vec2(5.2, 1.3) - t, detail));
  vec2 r = vec2(fbm(p + 3.2*q + vec2(1.7, 9.2) + t*1.3, detail), fbm(p + 3.2*q + vec2(8.3, 2.8) - t*.9, detail));
  float f = fbm(p + 3.*r, detail);
  vec3 col = mix(uA, uB, smoothstep(.15, .55, f));
  col = mix(col, uC, smoothstep(.45, 1.05, length(q)) * .85);
  col = mix(col, uD, smoothstep(.42, .82, r.x) * .85);
  col = mix(col, uE, smoothstep(.68, .95, f) * .75);
  col += pow(smoothstep(.55, .95, f * r.x * 1.6), 3.) * .14;
  vec3 paper = uDark > .5 ? vec3(.14) : vec3(.93, .925, .915);
  if (uDark > .5) col *= .82;
  // Out of focus it is the same pale grey whatever the palette, so a new one
  // can come in unseen.
  vec3 soft = mix(paper, paper * (uDark > .5 ? 1.45 : .84), smoothstep(.2, .8, f));
  col = mix(soft, col, uP);
  col += (hash(gl_FragCoord.xy + fract(uTime*.07)*917.) - .5) * .04;
  gl_FragColor = vec4(col, 1.);
}`

const hex = (h: string) => [1, 3, 5].map((i) => parseInt(h.slice(i, i + 2), 16) / 255)

/** A palette a round comes into focus in: sunset, deep sea, forest, morning mist, amber, twilight. */
const PALETTES = [
  ['#2b1d4a', '#c2416b', '#f08a4b', '#ffd27a', '#fff1d6'],
  ['#06233d', '#0b6e8a', '#2fb6a7', '#7fd3e8', '#e6fbff'],
  ['#14301f', '#2f6b3f', '#8fbf5a', '#e0c55b', '#f6f0cf'],
  ['#4a2340', '#b2588a', '#f2a7b8', '#c6b7f2', '#fff0f4'],
  ['#3a1a0a', '#a2481b', '#e8913a', '#f6c96b', '#fff4dc'],
  ['#101a3d', '#3446a8', '#6e7ff0', '#e07ac0', '#ffe3f2'],
].map((set) => set.map(hex))

const CYCLE = 9000
const ease = (x: number) => (x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2)
/** How far in focus a round is: rising, held, falling back. */
const focusAt = (ms: number) => {
  const ph = (ms % CYCLE) / CYCLE
  return ph < 0.62 ? ease(ph / 0.62) : ph < 0.8 ? 1 : 1 - ease((ph - 0.8) / 0.2)
}

export default function DevelopingPicture({ seed }: { seed: number }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // A canvas of this effect's own: a context given up on cleanup is lost for
    // good, so a canvas kept across effects (React runs them twice in
    // development) would be left blank.
    const host = ref.current
    if (!host) return
    const canvas = document.createElement('canvas')
    canvas.className = 'n-picture-canvas'
    canvas.setAttribute('aria-hidden', 'true')
    const gl = canvas.getContext('webgl', { antialias: false, premultipliedAlpha: false })
    if (!gl) return
    host.append(canvas)
    const compile = (type: number, source: string) => {
      const shader = gl.createShader(type)!
      gl.shaderSource(shader, source)
      gl.compileShader(shader)
      return shader
    }
    const program = gl.createProgram()!
    gl.attachShader(program, compile(gl.VERTEX_SHADER, VERTEX))
    gl.attachShader(program, compile(gl.FRAGMENT_SHADER, FRAGMENT))
    gl.linkProgram(program)
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      canvas.remove()
      return
    }
    gl.useProgram(program)
    gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer())
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW)
    const position = gl.getAttribLocation(program, 'p')
    gl.enableVertexAttribArray(position)
    gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0)
    const uniform = (name: string) => gl.getUniformLocation(program, name)
    const u = {
      res: uniform('uRes'), time: uniform('uTime'), p: uniform('uP'), dark: uniform('uDark'),
      colours: ['uA', 'uB', 'uC', 'uD', 'uE'].map(uniform),
    }

    const fit = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      canvas.width = Math.max(1, Math.round(canvas.clientWidth * dpr))
      canvas.height = Math.max(1, Math.round(canvas.clientHeight * dpr))
    }
    const resize = new ResizeObserver(fit)
    resize.observe(canvas)
    fit()

    // With reduced motion the field does not flow; it still comes into focus,
    // slowly - this is what says the picture is being made.
    const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const started = performance.now() - (seed % CYCLE)
    let visible = true
    let frame = 0
    const draw = (now: number) => {
      frame = 0
      const elapsed = now - started
      const round = Math.floor(elapsed / CYCLE) + seed
      const palette = PALETTES[((round % PALETTES.length) + PALETTES.length) % PALETTES.length]!
      gl.viewport(0, 0, canvas.width, canvas.height)
      gl.uniform2f(u.res, canvas.width, canvas.height)
      gl.uniform1f(u.time, still ? 0 : elapsed / 100)
      gl.uniform1f(u.p, focusAt(still ? elapsed / 2 : elapsed))
      gl.uniform1f(u.dark, document.documentElement.classList.contains('dark') ? 1 : 0)
      palette.forEach((colour, i) => gl.uniform3fv(u.colours[i]!, colour))
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4)
      if (visible && !document.hidden) frame = requestAnimationFrame(draw)
    }
    const resume = () => {
      if (visible && !document.hidden && !frame) frame = requestAnimationFrame(draw)
    }
    const seen = new IntersectionObserver(([entry]) => {
      visible = entry?.isIntersecting ?? true
      resume()
    })
    seen.observe(canvas)
    document.addEventListener('visibilitychange', resume)
    frame = requestAnimationFrame(draw)
    return () => {
      cancelAnimationFrame(frame)
      resize.disconnect()
      seen.disconnect()
      document.removeEventListener('visibilitychange', resume)
      gl.getExtension('WEBGL_lose_context')?.loseContext()
      canvas.remove()
    }
  }, [seed])

  return <div ref={ref} className="n-picture-canvas" aria-hidden="true" />
}
