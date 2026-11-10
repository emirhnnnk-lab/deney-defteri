import { useCallback, useEffect, useState } from 'react'

// Minimal data loader: { data, error, loading, reload }.
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const load = useCallback(fn, deps)

  const reload = useCallback(() => {
    setLoading(true)
    setError(null)
    load()
      .then(setData)
      .catch((e: Error) => setError(e.message))
      .finally(() => setLoading(false))
  }, [load])

  useEffect(reload, [reload])
  return { data, error, loading, reload }
}
