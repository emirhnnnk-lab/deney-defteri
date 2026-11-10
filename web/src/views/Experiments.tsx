import { useState } from 'react'
import { api, OUTCOMES, type Experiment, type ExperimentInput, type Outcome } from '../api'
import { useAsync } from '../useAsync'

const EMPTY: ExperimentInput = { title: '', prediction: '', action: '', result: '', lesson: '', outcome: 'pending', source: '' }

export function Experiments() {
  const { data, error, loading, reload } = useAsync(api.experiments)
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<Experiment | null>(null)
  const [filter, setFilter] = useState<Outcome | 'all'>('all')

  const rows = (data ?? []).filter((e) => filter === 'all' || e.outcome === filter)
  const counts = OUTCOMES.map((o) => [o, (data ?? []).filter((e) => e.outcome === o).length] as const)

  return (
    <section>
      <div className="toolbar">
        <div className="chips">
          <button className={filter === 'all' ? 'chip on' : 'chip'} onClick={() => setFilter('all')}>
            all {data?.length ?? 0}
          </button>
          {counts.map(([o, n]) => (
            <button key={o} className={filter === o ? `chip on ${o}` : `chip ${o}`} onClick={() => setFilter(o)}>
              {o} {n}
            </button>
          ))}
        </div>
        <button className="primary" onClick={() => setCreating(true)}>New experiment</button>
      </div>

      {loading && <p className="muted">Loading…</p>}
      {error && <p className="error">{error}</p>}

      {creating && (
        <ExperimentForm
          initial={EMPTY}
          isNew
          onCancel={() => setCreating(false)}
          onSave={async (v) => {
            await api.createExperiment(v)
            setCreating(false)
            reload()
          }}
        />
      )}

      <div className="cards">
        {rows.map((e) =>
          editing?.id === e.id ? (
            <ExperimentForm
              key={e.id}
              initial={e}
              onCancel={() => setEditing(null)}
              onSave={async (v) => {
                await api.updateExperiment(e.id, v)
                setEditing(null)
                reload()
              }}
            />
          ) : (
            <ExperimentCard key={e.id} e={e} onEdit={() => setEditing(e)} />
          ),
        )}
        {!loading && rows.length === 0 && <p className="muted">No experiments here yet.</p>}
      </div>
    </section>
  )
}

function ExperimentCard({ e, onEdit }: { e: Experiment; onEdit: () => void }) {
  const link = e.source.startsWith('http')
  return (
    <article className="card">
      <div className="card-head">
        <h3>{e.title}</h3>
        <span className={`badge ${e.outcome}`}>{e.outcome}</span>
      </div>
      <dl className="flow">
        <dt>Prediction</dt>
        <dd>{e.prediction}</dd>
        <dt>Action</dt>
        <dd>{e.action || <span className="muted">—</span>}</dd>
        <dt>Result</dt>
        <dd>{e.result || <span className="muted">not run yet</span>}</dd>
        <dt>Lesson</dt>
        <dd>{e.lesson || <span className="muted">—</span>}</dd>
      </dl>
      <div className="card-foot">
        <span className="muted">
          {new Date(e.created_at).toLocaleDateString()}
          {e.source && ' · '}
          {e.source && (link ? <a href={e.source} target="_blank" rel="noreferrer">{e.source.replace('https://github.com/', '')}</a> : e.source)}
        </span>
        <button className="ghost" onClick={onEdit}>Edit</button>
      </div>
    </article>
  )
}

function ExperimentForm({
  initial,
  isNew = false,
  onSave,
  onCancel,
}: {
  initial: ExperimentInput
  isNew?: boolean
  onSave: (v: ExperimentInput) => Promise<void>
  onCancel: () => void
}) {
  const [v, setV] = useState<ExperimentInput>({
    title: initial.title, prediction: initial.prediction, action: initial.action,
    result: initial.result, lesson: initial.lesson, outcome: initial.outcome, source: initial.source,
  })
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const set = (k: keyof ExperimentInput) => (ev: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) =>
    setV({ ...v, [k]: ev.target.value })

  return (
    <form
      className="card form"
      onSubmit={async (ev) => {
        ev.preventDefault()
        setBusy(true)
        setErr(null)
        try {
          await onSave(v)
        } catch (e) {
          setErr((e as Error).message)
        } finally {
          setBusy(false)
        }
      }}
    >
      <label>Title<input value={v.title} onChange={set('title')} required maxLength={200} /></label>
      <label>
        Prediction {isNew ? <small>(write it before you run; it cannot be edited later)</small> : <small>(locked)</small>}
        <textarea value={v.prediction} onChange={set('prediction')} required={isNew} readOnly={!isNew} rows={2} />
      </label>
      <label>Action<textarea value={v.action} onChange={set('action')} rows={2} /></label>
      <label>Result<textarea value={v.result} onChange={set('result')} rows={2} /></label>
      <label>Lesson<textarea value={v.lesson} onChange={set('lesson')} rows={2} /></label>
      <div className="row">
        <label>Outcome
          <select value={v.outcome} onChange={set('outcome')}>
            {OUTCOMES.map((o) => <option key={o}>{o}</option>)}
          </select>
        </label>
        <label className="grow">Source<input value={v.source} onChange={set('source')} placeholder="repo URL or file" /></label>
      </div>
      {err && <p className="error">{err}</p>}
      <div className="row end">
        <button type="button" className="ghost" onClick={onCancel}>Cancel</button>
        <button className="primary" disabled={busy}>{busy ? 'Saving…' : 'Save'}</button>
      </div>
    </form>
  )
}
