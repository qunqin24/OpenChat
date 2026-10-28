import type { LibraryFile } from '@openchat/api-client'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { API_BASE_URL } from '@/lib/api'
import { TEXT_TYPE } from '@/lib/attachments'
import { fileLook } from '@/lib/file-look'
import { pagePreview } from '@/lib/file-preview'
import { FileBadge } from './FileBadge'

/** How much of a text file is read for its preview. */
const PREVIEW_BYTES = 2048

/**
 * A library card's picture of a file that is words: a small page of its first
 * lines fading out, or for a table its first rows. Read only once the card is
 * on screen; a file whose words cannot be had shows its badge instead (the
 * card's foot carries the badge otherwise).
 */
export function FilePagePreview({ file }: { file: LibraryFile }) {
  const ref = useRef<HTMLDivElement>(null)
  const [seen, setSeen] = useState(false)
  useEffect(() => {
    const element = ref.current
    if (!element || seen) return
    const observer = new IntersectionObserver(([entry]) => entry?.isIntersecting && setSeen(true), { rootMargin: '200px' })
    observer.observe(element)
    return () => observer.disconnect()
  }, [seen])

  const words = useQuery({
    // Not in query-keys and never persisted: a preview is read for the page.
    queryKey: ['file-preview', file.id],
    queryFn: async ({ signal }) => {
      if (file.contentType === TEXT_TYPE) {
        const response = await fetch(file.url, { signal })
        if (!response.ok) throw new Error(`file ${response.status}`)
        return (await response.text()).slice(0, PREVIEW_BYTES)
      }
      const response = await fetch(`${API_BASE_URL}/files/${encodeURIComponent(file.id)}/text?preview=true`, { signal, credentials: 'include' })
      if (!response.ok) throw new Error(`file ${response.status}`)
      return response.text()
    },
    enabled: seen,
    staleTime: Infinity,
    gcTime: 5 * 60_000,
    retry: false,
  })

  const look = fileLook(file.filename, file.contentType)
  const preview = words.data === undefined ? null : pagePreview(words.data, look === 'data')

  return (
    <div ref={ref} className="n-page-preview">
      {preview && preview.kind !== 'empty' && (
        <div className="n-page-paper" dir="auto" aria-hidden="true">
          {preview.kind === 'table' ? (
            <table>
              <tbody>
                {preview.rows.map((row, i) => (
                  <tr key={i}>
                    {row.map((cell, j) => (
                      <td key={j}>{cell}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <>
              {preview.title && <strong>{preview.title}</strong>}
              {preview.lines.map((line, i) => (
                <p key={i}>{line}</p>
              ))}
            </>
          )}
        </div>
      )}
      {preview?.kind !== 'page' && preview?.kind !== 'table' && words.isFetched && (
        <FileBadge filename={file.filename} contentType={file.contentType} />
      )}
    </div>
  )
}

export default FilePagePreview
