import { glyphColor } from '@/lib/plugin-icon'

interface PluginLook {
  name: string
  iconText: string
  iconColor: string
  iconPath: string
}

/** A plugin's mark: its brand icon, or its letters, on its colour. */
export default function PluginMark({ plugin, size }: { plugin: PluginLook; size: number }) {
  const style = { width: size, height: size, fontSize: Math.round(size * 0.42), ...(plugin.iconColor ? { background: plugin.iconColor, color: glyphColor(plugin.iconColor) } : {}) }
  return (
    <span className="n-plugin-mark" aria-hidden="true" style={style}>
      {plugin.iconPath ? (
        <svg viewBox="0 0 24 24" width={Math.round(size * 0.56)} height={Math.round(size * 0.56)} fill="currentColor">
          <path d={plugin.iconPath} />
        </svg>
      ) : (
        plugin.iconText || plugin.name.slice(0, 1)
      )}
    </span>
  )
}
