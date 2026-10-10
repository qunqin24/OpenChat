/**
 * The parser for OpenChat's UI language — the declarations inside an
 * `openchat-ui` block:
 *
 *     root = Card([summary, plan], title: "Launch readiness")
 *     summary = Text("Two of three workstreams are **on track**.")
 *
 * One declaration per line, `name = value`. A value is a JSON literal, an
 * array, an object, a component call with positional and then keyed
 * arguments, or the name of another declaration — which may come later, so a
 * model can write the outline first and fill it in.
 *
 * Built for streaming. A block is parsed again every time it grows, and a
 * declaration that is still being written is simply not there yet: only
 * complete lines are parsed while the block is open. A line that does not
 * parse is skipped on its own and reported; the rest of the block still draws.
 */

export type Value =
  | { kind: 'literal'; value: string | number | boolean | null }
  | { kind: 'array'; items: Value[] }
  | { kind: 'object'; entries: Record<string, Value> }
  | { kind: 'call'; name: string; positional: Value[]; named: Record<string, Value> }
  | { kind: 'ref'; name: string }

export interface Declaration {
  name: string
  value: Value
}

export interface ParseResult {
  /** In source order; a later declaration of the same name replaces an earlier one. */
  declarations: Map<string, Value>
  /** The first declaration's name: what the block shows. */
  entry: string | null
  /** Lines that could not be parsed, for diagnostics. */
  errors: { line: string; message: string }[]
}

/** Bounds against a runaway or hostile block. */
const MAX_SOURCE = 200_000
const MAX_DEPTH = 32

/**
 * Parses a block. `complete` is false while the block is still streaming, in
 * which case its unfinished last declaration is left out rather than reported.
 */
export function parseBlock(source: string, complete: boolean): ParseResult {
  const result: ParseResult = { declarations: new Map(), entry: null, errors: [] }
  const statements = splitStatements(source.slice(0, MAX_SOURCE), complete)
  for (const statement of statements) {
    const trimmed = statement.trim()
    if (!trimmed) continue
    try {
      const declaration = parseDeclaration(trimmed)
      if (result.entry === null) result.entry = declaration.name
      result.declarations.set(declaration.name, declaration.value)
    } catch (error) {
      result.errors.push({ line: trimmed, message: error instanceof Error ? error.message : String(error) })
    }
  }
  return result
}

/**
 * Cuts the source into declarations: at newlines that are outside strings and
 * outside brackets, so a long array may span lines. While streaming, whatever
 * follows the last such newline is still being written and is dropped.
 */
function splitStatements(source: string, complete: boolean): string[] {
  const statements: string[] = []
  let depth = 0
  let inString = false
  let escaped = false
  let start = 0
  for (let i = 0; i < source.length; i++) {
    const ch = source[i]
    if (inString) {
      if (escaped) escaped = false
      else if (ch === '\\') escaped = true
      else if (ch === '"') inString = false
      continue
    }
    if (ch === '"') inString = true
    else if (ch === '(' || ch === '[' || ch === '{') depth++
    else if (ch === ')' || ch === ']' || ch === '}') depth = Math.max(0, depth - 1)
    else if (ch === '\n' && depth === 0) {
      statements.push(source.slice(start, i))
      start = i + 1
    }
  }
  if (complete) statements.push(source.slice(start))
  return statements
}

// ---------------------------------------------------------------- tokens

type Token =
  | { type: 'ident'; text: string }
  | { type: 'string'; value: string }
  | { type: 'number'; value: number }
  | { type: 'punct'; text: string }

function tokenize(text: string): Token[] {
  const tokens: Token[] = []
  let i = 0
  while (i < text.length) {
    const ch = text[i]!
    if (/\s/.test(ch)) {
      i++
      continue
    }
    if ('()[]{},:='.includes(ch)) {
      tokens.push({ type: 'punct', text: ch })
      i++
      continue
    }
    if (ch === '"') {
      let j = i + 1
      let escaped = false
      for (; j < text.length; j++) {
        if (escaped) escaped = false
        else if (text[j] === '\\') escaped = true
        else if (text[j] === '"') break
      }
      if (j >= text.length) throw new Error('unterminated string')
      // JSON's own string grammar: escapes and all.
      tokens.push({ type: 'string', value: JSON.parse(text.slice(i, j + 1)) as string })
      i = j + 1
      continue
    }
    const number = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?/.exec(text.slice(i))
    if (number && (ch === '-' || /\d/.test(ch))) {
      tokens.push({ type: 'number', value: Number(number[0]) })
      i += number[0].length
      continue
    }
    const ident = /^[A-Za-z_][A-Za-z0-9_]*/.exec(text.slice(i))
    if (ident) {
      tokens.push({ type: 'ident', text: ident[0] })
      i += ident[0].length
      continue
    }
    throw new Error(`unexpected character ${JSON.stringify(ch)}`)
  }
  return tokens
}

// ---------------------------------------------------------------- grammar

function parseDeclaration(text: string): Declaration {
  const tokens = tokenize(text)
  const name = tokens[0]
  if (name?.type !== 'ident') throw new Error('a declaration starts with a name')
  const equals = tokens[1]
  if (equals?.type !== 'punct' || equals.text !== '=') throw new Error('expected = after the name')
  const parser = new Parser(tokens, 2)
  const value = parser.value(0)
  if (!parser.done()) throw new Error('unexpected text after the value')
  return { name: name.text, value }
}

class Parser {
  constructor(
    private tokens: Token[],
    private at: number,
  ) {}

  done() {
    return this.at >= this.tokens.length
  }

  private peek(offset = 0) {
    return this.tokens[this.at + offset]
  }

  private isPunct(text: string, offset = 0) {
    const token = this.peek(offset)
    return token?.type === 'punct' && token.text === text
  }

  private expect(text: string) {
    if (!this.isPunct(text)) throw new Error(`expected ${text}`)
    this.at++
  }

  value(depth: number): Value {
    if (depth > MAX_DEPTH) throw new Error('nested too deeply')
    const token = this.peek()
    if (!token) throw new Error('expected a value')
    if (token.type === 'string') {
      this.at++
      return { kind: 'literal', value: token.value }
    }
    if (token.type === 'number') {
      this.at++
      return { kind: 'literal', value: token.value }
    }
    if (token.type === 'punct') {
      if (token.text === '[') return this.array(depth)
      if (token.text === '{') return this.object(depth)
      throw new Error(`unexpected ${token.text}`)
    }
    // An identifier: a literal word, a call, or a reference.
    this.at++
    if (token.text === 'true' || token.text === 'false') return { kind: 'literal', value: token.text === 'true' }
    if (token.text === 'null') return { kind: 'literal', value: null }
    if (this.isPunct('(')) return this.call(token.text, depth)
    return { kind: 'ref', name: token.text }
  }

  private array(depth: number): Value {
    this.expect('[')
    const items: Value[] = []
    while (!this.isPunct(']')) {
      items.push(this.value(depth + 1))
      if (!this.isPunct(',')) break
      this.at++
    }
    this.expect(']')
    return { kind: 'array', items }
  }

  private object(depth: number): Value {
    this.expect('{')
    const entries: Record<string, Value> = {}
    while (!this.isPunct('}')) {
      const key = this.peek()
      if (key?.type !== 'ident' && key?.type !== 'string') throw new Error('expected a key')
      this.at++
      this.expect(':')
      entries[key.type === 'ident' ? key.text : key.value] = this.value(depth + 1)
      if (!this.isPunct(',')) break
      this.at++
    }
    this.expect('}')
    return { kind: 'object', entries }
  }

  private call(name: string, depth: number): Value {
    this.expect('(')
    const positional: Value[] = []
    const named: Record<string, Value> = {}
    while (!this.isPunct(')')) {
      const token = this.peek()
      if (token?.type === 'ident' && this.isPunct(':', 1)) {
        this.at += 2
        named[token.text] = this.value(depth + 1)
      } else {
        if (Object.keys(named).length > 0) throw new Error('positional arguments come before keyed ones')
        positional.push(this.value(depth + 1))
      }
      if (!this.isPunct(',')) break
      this.at++
    }
    this.expect(')')
    return { kind: 'call', name, positional, named }
  }
}
