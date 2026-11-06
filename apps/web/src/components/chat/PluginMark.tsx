import { glyphColor } from '@/lib/plugin-icon'

interface PluginLook {
  name: string
  iconText: string
  iconColor: string
  /** The glyph's own colour, where the brand's is not the dark or white its background would choose. */
  iconInk?: string
  iconPath: string
}

/** A plugin's mark: its brand icon, or its letters, on its colour, in its ink when it has one. */
export default function PluginMark({ plugin, size }: { plugin: PluginLook; size: number }) {
  const style = { width: size, height: size, fontSize: Math.round(size * 0.42), ...(plugin.iconColor ? { background: plugin.iconColor, color: plugin.iconInk || glyphColor(plugin.iconColor) } : {}) }
  return (
    <span className="n-plugin-mark" aria-hidden="true" style={style}>
      {plugin.iconPath ? (
        // overflow: a mark that is a badge of its own (Luckin's) runs past the
        // 24 x 24 box to fill the tile; others stay inside it.
        <svg viewBox="0 0 24 24" width={Math.round(size * 0.56)} height={Math.round(size * 0.56)} fill="currentColor" overflow="visible">
          <path d={plugin.iconPath} />
        </svg>
      ) : (
        plugin.iconText || plugin.name.slice(0, 1)
      )}
    </span>
  )
}
