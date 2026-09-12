import { toast } from 'sonner'

/**
 * Upstream's demo affordance: echoes a submitted form back as a toast. Kept so
 * the vendored dialogs compile unchanged; the dialogs that reach real
 * endpoints call the API instead.
 */
export function showSubmittedData(data: unknown, title = 'You submitted the following values:') {
  toast.message(title, {
    description: (
      <pre className='mt-2 w-[340px] overflow-x-auto rounded-md bg-slate-950 p-4'>
        <code className='text-white'>{JSON.stringify(data, null, 2)}</code>
      </pre>
    ),
  })
}
