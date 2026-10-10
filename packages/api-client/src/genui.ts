import catalog from './genui-catalog.gen.json'

/**
 * The generative UI component catalogue: which components a model may use in
 * an `openchat-ui` block, and what each one's parameters accept.
 *
 * Generated from api/genui-catalog.json, the same file the server builds the
 * model's instructions from, so what the model is told and what the page will
 * draw are one list.
 */
export type GenUIParamType =
  | 'string'
  | 'markdown'
  | 'integer'
  | 'number'
  | 'boolean'
  | 'enum'
  | 'strings'
  | 'numbers'
  | 'cells'
  | 'pairs'
  | 'node'
  | 'nodes'

export interface GenUIParam {
  name: string
  type: GenUIParamType
  required?: boolean
  values?: string[]
  of?: string[]
  min?: number
  max?: number
  default?: string | number | boolean
}

export interface GenUIComponent {
  name: string
  description: string
  params: GenUIParam[]
}

export interface GenUICatalog {
  version: number
  components: GenUIComponent[]
}

export const genuiCatalog = catalog as GenUICatalog
