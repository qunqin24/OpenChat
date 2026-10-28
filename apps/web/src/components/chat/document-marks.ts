import { Extension } from '@tiptap/react'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { Decoration, DecorationSet } from '@tiptap/pm/view'

/** Which top-level blocks to mark, and how. */
export interface BlockMarks {
  /** Being rewritten by the model: drawn as a shimmering placeholder. */
  rewriting: number[]
  /** Just changed by the model: a highlight that fades. */
  fresh: number[]
}

export const blockMarksKey = new PluginKey<DecorationSet>('documentBlockMarks')

/**
 * Marks whole blocks of a writing block - set with
 * `tr.setMeta(blockMarksKey, marks)` - without touching the document: the
 * placeholder over a passage the model is rewriting, and the fading highlight
 * on what it changed.
 */
export const DocumentBlockMarks = Extension.create({
  name: 'documentBlockMarks',
  addProseMirrorPlugins() {
    return [
      new Plugin<DecorationSet>({
        key: blockMarksKey,
        state: {
          init: () => DecorationSet.empty,
          apply(tr, set) {
            const marks = tr.getMeta(blockMarksKey) as BlockMarks | undefined
            if (!marks) return set.map(tr.mapping, tr.doc)
            const decorations: Decoration[] = []
            let index = 0
            tr.doc.forEach((node, offset) => {
              const rewriting = marks.rewriting.includes(index)
              const fresh = marks.fresh.includes(index)
              if (rewriting || fresh) {
                decorations.push(Decoration.node(
                  offset,
                  offset + node.nodeSize,
                  rewriting
                    ? { class: 'n-doc-rewriting', 'data-motion': 'essential', 'aria-busy': 'true' }
                    : { class: 'n-doc-fresh' },
                  { kind: rewriting ? 'rewriting' : 'fresh' },
                ))
              }
              index++
            })
            return DecorationSet.create(tr.doc, decorations)
          },
        },
        props: {
          decorations(state) {
            return blockMarksKey.getState(state)
          },
        },
      }),
    ]
  },
})
