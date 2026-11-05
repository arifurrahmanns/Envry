import { useCallback } from 'react'
import { useSearchParams } from 'react-router'

/** The current `?q=` search term, shared by the header and the lists. */
export function useSearchQuery(): [string, (q: string) => void] {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const setQ = useCallback(
    (next: string) =>
      setParams(
        (prev) => {
          const p = new URLSearchParams(prev)
          if (next) p.set('q', next)
          else p.delete('q')
          p.delete('m')
          return p
        },
        { replace: true },
      ),
    [setParams],
  )
  return [q, setQ]
}

/** Read and update a single search param without touching the others. */
export function useParam(name: string): [string | null, (value: string | null) => void] {
  const [params, setParams] = useSearchParams()
  const value = params.get(name)
  const set = useCallback(
    (next: string | null) =>
      setParams((prev) => {
        const p = new URLSearchParams(prev)
        if (next === null || next === '') p.delete(name)
        else p.set(name, next)
        return p
      }),
    [name, setParams],
  )
  return [value, set]
}
