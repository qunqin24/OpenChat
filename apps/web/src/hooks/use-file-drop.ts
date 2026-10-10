import { useCallback, useEffect, useRef, useState, type DragEvent as ReactDragEvent } from 'react'

function carriesFiles(transfer: DataTransfer | null) {
  return Array.from(transfer?.types ?? []).includes('Files')
}

/** File drag feedback for a mounted drop zone; accepting/uploading is the caller's job. */
export function useFileDrop(onFiles: (files: File[]) => void) {
  const depth = useRef(0)
  const [dragging, setDragging] = useState(false)

  const reset = useCallback(() => {
    depth.current = 0
    setDragging(false)
  }, [])

  useEffect(() => {
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') reset()
    }
    const handleVisibility = () => {
      if (document.hidden) reset()
    }
    // While chat is mounted, an unhandled file drop must not navigate the tab
    // away from the app. The zone's own handler decides what to do with files.
    const preventFileNavigation = (event: DragEvent) => {
      if (carriesFiles(event.dataTransfer)) event.preventDefault()
    }
    const handleWindowDrop = (event: DragEvent) => {
      preventFileNavigation(event)
      reset()
    }

    window.addEventListener('dragover', preventFileNavigation, true)
    window.addEventListener('drop', handleWindowDrop, true)
    window.addEventListener('dragend', reset)
    window.addEventListener('blur', reset)
    window.addEventListener('keydown', handleKey)
    document.addEventListener('visibilitychange', handleVisibility)
    return () => {
      window.removeEventListener('dragover', preventFileNavigation, true)
      window.removeEventListener('drop', handleWindowDrop, true)
      window.removeEventListener('dragend', reset)
      window.removeEventListener('blur', reset)
      window.removeEventListener('keydown', handleKey)
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [reset])

  const handlers = {
    onDragEnter(event: ReactDragEvent<HTMLElement>) {
      if (!carriesFiles(event.dataTransfer)) return
      event.preventDefault()
      depth.current += 1
      setDragging(true)
    },
    onDragOver(event: ReactDragEvent<HTMLElement>) {
      if (!carriesFiles(event.dataTransfer)) return
      event.preventDefault()
      // Some browser drag sources begin with dragover instead of dragenter.
      if (depth.current === 0) depth.current = 1
      setDragging(true)
    },
    onDragLeave(event: ReactDragEvent<HTMLElement>) {
      if (!carriesFiles(event.dataTransfer) && depth.current === 0) return
      // Always balance nested enters, even for an internal move. Returning
      // before decrementing would leave the overlay stuck on the final exit.
      depth.current = Math.max(0, depth.current - 1)
      if (event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget)) return
      if (depth.current === 0) setDragging(false)
    },
    onDrop(event: ReactDragEvent<HTMLElement>) {
      if (!carriesFiles(event.dataTransfer)) return
      event.preventDefault()
      reset()
      const files = Array.from(event.dataTransfer.files)
      if (files.length) onFiles(files)
    },
  }

  return { dragging, handlers }
}
