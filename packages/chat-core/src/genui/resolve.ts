import { genuiCatalog, type GenUIComponent, type GenUIParam } from '@openchat/api-client'

import type { ParseResult, Value } from './parse'

/**
 * A component ready to draw: its catalogue entry's name and its parameters,
 * each checked against the catalogue and converted to what the renderer
 * expects. Anything the catalogue does not allow has been dropped by now — an
 * unknown component, an unknown parameter, a value of the wrong kind — so the
 * renderer never sees model output it was not built for.
 */
export interface UINode {
  /** Stable across re-parses while streaming: the declaration path. */
  key: string
  component: string
  props: Record<string, unknown>
}

export type Cell = string | number

export interface Resolved {
  root: UINode | null
  /** What was dropped and why, for diagnostics. */
  issues: string[]
}

const components = new Map(genuiCatalog.components.map((c) => [c.name, c]))

/** Bounds against a block that would draw without end. */
const MAX_NODES = 400

/**
 * Builds the tree from a parsed block. While streaming (`complete` false), a
 * reference to a declaration not written yet, or a component still missing a
 * required value, is left out quietly — it is coming. Once the block is
 * complete, the same things are reported.
 */
export function resolveBlock(parsed: ParseResult, complete: boolean): Resolved {
  const issues: string[] = []
  let count = 0

  const report = (message: string) => {
    if (complete) issues.push(message)
  }

  // `path` holds the declarations being resolved, to break a cycle; `key` is
  // the position in the tree, which keeps React's keys stable as it grows.
  const node = (value: Value, key: string, path: string[], allowed?: string[]): UINode | null => {
    if (value.kind === 'ref') {
      if (path.includes(value.name)) {
        issues.push(`${value.name} refers to itself`)
        return null
      }
      const target = parsed.declarations.get(value.name)
      if (!target) {
        report(`${value.name} is used but never declared`)
        return null
      }
      return node(target, `${key}/${value.name}`, [...path, value.name], allowed)
    }
    if (value.kind !== 'call') {
      report(`${key}: expected a component`)
      return null
    }
    const component = components.get(value.name)
    if (!component) {
      issues.push(`unknown component ${value.name}`)
      return null
    }
    if (allowed && !allowed.includes(component.name)) {
      issues.push(`${component.name} is not allowed here; expected ${allowed.join(' or ')}`)
      return null
    }
    if (++count > MAX_NODES) {
      issues.push('too many components')
      return null
    }

    const props: Record<string, unknown> = {}
    const given = bindArguments(component, value, issues)
    for (const param of component.params) {
      const raw = given.get(param.name)
      const converted =
        raw === undefined ? undefined : convert(param, raw, `${key}.${param.name}`, path)
      if (converted === undefined) {
        if (param.required) {
          report(`${component.name} is missing ${param.name}`)
          return null
        }
        if (param.default !== undefined) props[param.name] = param.default
        continue
      }
      props[param.name] = converted
    }
    return { key, component: component.name, props }
  }

  const convert = (param: GenUIParam, value: Value, key: string, path: string[]): unknown => {
    switch (param.type) {
      case 'node':
        return node(value, key, path, param.of) ?? undefined
      case 'nodes': {
        const items = list(value, path)
        if (!items) return undefined
        return items
          .map((item, i) => node(item, `${key}[${i}]`, path, param.of))
          .filter((n): n is UINode => n !== null)
      }
      default: {
        const plain = toPlain(value, path)
        return plain === undefined ? undefined : coerce(param, plain)
      }
    }
  }

  // A list may itself be a reference to a declared array.
  const list = (value: Value, path: string[]): Value[] | null => {
    if (value.kind === 'array') return value.items
    if (value.kind === 'ref' && !path.includes(value.name)) {
      const target = parsed.declarations.get(value.name)
      return target ? list(target, [...path, value.name]) : null
    }
    return null
  }

  // Plain data: literals, arrays and objects, with references followed.
  const toPlain = (value: Value, path: string[]): unknown => {
    switch (value.kind) {
      case 'literal':
        return value.value
      case 'array':
        return value.items.map((item) => toPlain(item, path))
      case 'object':
        return Object.fromEntries(Object.entries(value.entries).map(([k, v]) => [k, toPlain(v, path)]))
      case 'ref': {
        if (path.includes(value.name)) return undefined
        const target = parsed.declarations.get(value.name)
        return target ? toPlain(target, [...path, value.name]) : undefined
      }
      case 'call':
        return undefined
    }
  }

  const entry = parsed.declarations.has('root') ? 'root' : parsed.entry
  const root = entry ? node({ kind: 'ref', name: entry }, 'root', []) : null
  return { root, issues }
}

/** Positional arguments fill parameters in catalogue order; keyed ones by name. */
function bindArguments(component: GenUIComponent, call: Extract<Value, { kind: 'call' }>, issues: string[]) {
  const given = new Map<string, Value>()
  call.positional.forEach((value, i) => {
    const param = component.params[i]
    if (param) given.set(param.name, value)
    else issues.push(`${component.name} takes ${component.params.length} arguments`)
  })
  for (const [name, value] of Object.entries(call.named)) {
    if (component.params.some((p) => p.name === name)) given.set(name, value)
    else issues.push(`${component.name} has no parameter ${name}`)
  }
  return given
}

/** Converts plain data to a parameter's type, or undefined when it cannot be. */
function coerce(param: GenUIParam, value: unknown): unknown {
  switch (param.type) {
    case 'string':
    case 'markdown':
      return typeof value === 'string' ? value : typeof value === 'number' ? String(value) : undefined
    case 'integer':
    case 'number': {
      const n = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : NaN
      if (!Number.isFinite(n)) return undefined
      let out = param.type === 'integer' ? Math.round(n) : n
      if (param.min !== undefined) out = Math.max(param.min, out)
      if (param.max !== undefined) out = Math.min(param.max, out)
      return out
    }
    case 'boolean':
      return typeof value === 'boolean' ? value : undefined
    case 'enum':
      return typeof value === 'string' && param.values?.includes(value) ? value : undefined
    case 'strings':
      return Array.isArray(value) ? value.filter(isCell).map(String) : undefined
    case 'numbers':
      // A value that is not a number keeps its place as NaN, so a series
      // stays aligned with its labels; the chart leaves a gap there.
      return Array.isArray(value)
        ? value.map((v) => (typeof v === 'number' ? v : typeof v === 'string' && v.trim() !== '' ? Number(v) : NaN))
        : undefined
    case 'cells':
      return Array.isArray(value)
        ? value.filter(Array.isArray).map((row: unknown[]) => row.map((c) => (isCell(c) ? c : '')))
        : undefined
    case 'pairs':
      return Array.isArray(value)
        ? value
            .map((pair: unknown) => {
              if (Array.isArray(pair) && pair.length >= 2) return [String(pair[0]), String(pair[1])]
              if (pair && typeof pair === 'object' && 'label' in pair && 'value' in pair) {
                const p = pair as { label: unknown; value: unknown }
                return [String(p.label), String(p.value)]
              }
              return null
            })
            .filter((p): p is [string, string] => p !== null)
        : undefined
    default:
      return undefined
  }
}

function isCell(value: unknown): value is Cell {
  return typeof value === 'string' || typeof value === 'number'
}
