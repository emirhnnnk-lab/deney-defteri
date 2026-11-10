import { useEffect, useState } from 'react'
import { api, pct, sec, type Comparison, type Dimension } from '../api'
import { useAsync } from '../useAsync'

export function ABView() {
  const [dim, setDim] = useState<Dimension>('model')
  const opts = useAsync(() => api.abOptions(dim), [dim])
  const [a, setA] = useState('')
  const [b, setB] = useState('')
  const [exp, setExp] = useState('')
  const [cmp, setCmp] = useState<Comparison | null>(null)
  const [err, setErr] = useState<string | null>(null)

  // Pick sensible defaults when the options load: first experiment, first two values.
  useEffect(() => {
    const v = opts.data?.values ?? []
    setA(v[0] ?? '')
    setB(v[1] ?? '')
    setExp(opts.data?.experiments[0] ?? '')
    setCmp(null)
  }, [opts.data])

  useEffect(() => {
    if (!a || !b || a === b) return
    setErr(null)
    api.compare(dim, a, b, exp).then(setCmp).catch((e: Error) => setErr(e.message))
  }, [dim, a, b, exp])

  const values = opts.data?.values ?? []

  return (
    <section>
      <div className="toolbar wrap">
        <label>Compare by
          <select value={dim} onChange={(e) => setDim(e.target.value as Dimension)}>
            <option value="model">model</option>
            <option value="provider">provider</option>
            <option value="task">task</option>
          </select>
        </label>
        <label>A
          <select value={a} onChange={(e) => setA(e.target.value)}>
            {values.map((v) => <option key={v}>{v}</option>)}
          </select>
        </label>
        <label>B
          <select value={b} onChange={(e) => setB(e.target.value)}>
            {values.map((v) => <option key={v}>{v}</option>)}
          </select>
        </label>
        <label>Experiment
          <select value={exp} onChange={(e) => setExp(e.target.value)}>
            <option value="">all calls</option>
            {(opts.data?.experiments ?? []).map((x) => <option key={x}>{x}</option>)}
          </select>
        </label>
      </div>

      {values.length < 2 && !opts.loading && <p className="muted">Need at least two values to compare. Import more calls first.</p>}
      {a && a === b && <p className="muted">Pick two different values.</p>}
      {err && <p className="error">{err}</p>}

      {cmp && (
        <>
          {cmp.warnings.map((w) => <p key={w} className="warn">⚠ {w}</p>)}
          <div className="ab-grid">
            <Metric title="Accuracy" a={pct(cmp.accuracy_a)} b={pct(cmp.accuracy_b)}
              av={cmp.accuracy_a} bv={cmp.accuracy_b} higherIsBetter
              foot={cmp.accuracy_diff == null ? 'no graded calls'
                : `B − A = ${(cmp.accuracy_diff * 100).toFixed(1)} pts · p = ${cmp.accuracy_p_value == null ? 'n/a' : cmp.accuracy_p_value.toFixed(3)}`} />
            <Metric title="Mean latency" a={sec(cmp.a.mean_sec)} b={sec(cmp.b.mean_sec)}
              av={cmp.a.mean_sec} bv={cmp.b.mean_sec}
              foot={(cmp.speed_ratio == null ? '' : cmp.speed_ratio >= 1
                ? `B is ${cmp.speed_ratio.toFixed(2)}× faster on the mean` : `A is ${(1 / cmp.speed_ratio).toFixed(2)}× faster on the mean`)
                + ` · median ${sec(cmp.a.median_sec)} vs ${sec(cmp.b.median_sec)} (check both: one slow outlier moves the mean)`} />
            <Metric title="Call success" a={pct(cmp.success_rate_a)} b={pct(cmp.success_rate_b)}
              av={cmp.success_rate_a} bv={cmp.success_rate_b} higherIsBetter
              foot={`${cmp.a.calls} vs ${cmp.b.calls} calls`} />
          </div>
          <p className="muted small">
            Accuracy uses calls with a gold label (<code>dogru</code> in the log). p is a two-sided pooled two-proportion z-test;
            repeated runs of the same item are not independent, so read it as a rough guide.
          </p>
        </>
      )}
    </section>
  )
}

function Metric({ title, a, b, av, bv, foot, higherIsBetter = false }: {
  title: string; a: string; b: string; av: number | null; bv: number | null; foot: string; higherIsBetter?: boolean
}) {
  const max = Math.max(av ?? 0, bv ?? 0) || 1
  const winner = av == null || bv == null || av === bv ? null : (av > bv) === higherIsBetter ? 'a' : 'b'
  return (
    <div className="metric">
      <h3>{title}</h3>
      {(['a', 'b'] as const).map((side) => {
        const v = side === 'a' ? av : bv
        return (
          <div key={side} className={`metric-row ${winner === side ? 'win' : ''}`}>
            <span className="side">{side.toUpperCase()}</span>
            <div className="track"><div className={`fill ${side}`} style={{ width: `${((v ?? 0) / max) * 100}%` }} /></div>
            <span className="val">{side === 'a' ? a : b}</span>
          </div>
        )
      })}
      <p className="muted small">{foot}</p>
    </div>
  )
}
