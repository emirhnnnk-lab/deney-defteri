import { useEffect, useState } from 'react'
import { Experiments } from './views/Experiments'
import { Usage } from './views/Usage'
import { ABView } from './views/ABView'

const TABS = [
  { id: 'experiments', label: 'Experiments' },
  { id: 'usage', label: 'Model usage' },
  { id: 'ab', label: 'A/B compare' },
] as const
type Tab = (typeof TABS)[number]['id']

const fromHash = (): Tab => {
  const h = window.location.hash.slice(1)
  return TABS.some((t) => t.id === h) ? (h as Tab) : 'experiments'
}

export default function App() {
  // The tab lives in the URL hash so a link can open a specific view.
  const [tab, setTabState] = useState<Tab>(fromHash)
  useEffect(() => {
    const onHash = () => setTabState(fromHash())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])
  const setTab = (t: Tab) => {
    window.location.hash = t
    setTabState(t)
  }
  return (
    <div className="shell">
      <header>
        <div>
          <h1>Deney Defteri</h1>
          <p className="sub">Experiment notebook: prediction → action → result → lesson</p>
        </div>
        <nav role="tablist">
          {TABS.map((t) => (
            <button
              key={t.id}
              role="tab"
              aria-selected={tab === t.id}
              className={tab === t.id ? 'tab active' : 'tab'}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </nav>
      </header>
      <main>
        {tab === 'experiments' && <Experiments />}
        {tab === 'usage' && <Usage />}
        {tab === 'ab' && <ABView />}
      </main>
    </div>
  )
}
