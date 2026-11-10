import { useState } from 'react'
import { api, pct, sec, type ImportResult } from '../api'
import { useAsync } from '../useAsync'

export function Usage() {
  const stats = useAsync(api.modelStats)
  const events = useAsync(() => api.events(100))
  const [imp, setImp] = useState<ImportResult | null>(null)
  const [impErr, setImpErr] = useState<string | null>(null)

  const runImport = async () => {
    setImpErr(null)
    try {
      setImp(await api.importLog())
      stats.reload()
      events.reload()
    } catch (e) {
      setImpErr((e as Error).message)
    }
  }

  const maxMean = Math.max(0.001, ...(stats.data ?? []).map((m) => m.mean_sec ?? 0))

  return (
    <section>
      <div className="toolbar">
        <p className="muted">Every call made through the free-model bridge, imported from its JSONL log.</p>
        <button className="primary" onClick={runImport}>Import log</button>
      </div>
      {imp && (
        <p className="note">
          Read {imp.read} rows, {imp.inserted} new{imp.skipped.length > 0 && `, ${imp.skipped.length} skipped (first: line ${imp.skipped[0].line}, ${imp.skipped[0].reason})`}.
        </p>
      )}
      {impErr && <p className="error">{impErr}</p>}

      <h2>Per model</h2>
      {stats.error && <p className="error">{stats.error}</p>}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Model</th><th>Provider</th><th className="num">Calls</th><th className="num">Success</th>
              <th className="num">Accuracy</th><th>Mean latency</th><th className="num">Median</th>
            </tr>
          </thead>
          <tbody>
            {(stats.data ?? []).map((m) => (
              <tr key={m.model + m.provider}>
                <td><code>{m.model}</code></td>
                <td>{m.provider}</td>
                <td className="num">{m.calls}</td>
                <td className="num">{pct(m.success_rate)}</td>
                <td className="num">{m.graded ? `${pct(m.accuracy)} (${m.correct}/${m.graded})` : '—'}</td>
                <td>
                  <div className="bar-cell">
                    <div className="bar" style={{ width: `${((m.mean_sec ?? 0) / maxMean) * 100}%` }} />
                    <span>{sec(m.mean_sec)}</span>
                  </div>
                </td>
                <td className="num">{sec(m.median_sec)}</td>
              </tr>
            ))}
            {stats.data?.length === 0 && (
              <tr><td colSpan={7} className="muted">No calls imported yet. Press “Import log”.</td></tr>
            )}
          </tbody>
        </table>
      </div>

      <h2>Recent calls</h2>
      <div className="table-wrap">
        <table>
          <thead>
            <tr><th>Time</th><th>Task</th><th>Model</th><th className="num">Latency</th><th>Status</th><th>Experiment</th></tr>
          </thead>
          <tbody>
            {(events.data ?? []).map((e, i) => (
              <tr key={i}>
                <td>{new Date(e.occurred_at).toLocaleString()}</td>
                <td>{e.task}</td>
                <td><code>{e.model}</code></td>
                <td className="num">{sec(e.duration_sec)}</td>
                <td>
                  {e.success ? <span className="ok">ok</span> : <span className="bad">failed{e.error ? ` (${e.error})` : ''}</span>}
                  {e.correct != null && (e.correct ? <span className="ok"> · correct</span> : <span className="bad"> · wrong</span>)}
                </td>
                <td className="muted">{e.experiment || ''}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
