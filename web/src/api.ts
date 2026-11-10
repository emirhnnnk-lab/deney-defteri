// Typed client for the Go API. Types mirror the JSON tags in backend/internal.

export type Outcome = 'pending' | 'confirmed' | 'refuted' | 'inconclusive'
export const OUTCOMES: Outcome[] = ['pending', 'confirmed', 'refuted', 'inconclusive']

export interface Experiment {
  id: number
  slug: string | null
  title: string
  prediction: string
  action: string
  result: string
  lesson: string
  outcome: Outcome
  source: string
  created_at: string
  decided_at: string | null
}

export type ExperimentInput = Pick<Experiment, 'title' | 'prediction' | 'action' | 'result' | 'lesson' | 'outcome' | 'source'>

export interface UsageEvent {
  task: string
  provider: string
  model: string
  success: boolean
  duration_sec: number | null
  input_chars: number | null
  output_chars: number | null
  prompt_tokens: number | null
  completion_tokens: number | null
  error?: string
  experiment?: string
  correct: boolean | null
  occurred_at: string
}

export interface ModelStat {
  model: string
  provider: string
  calls: number
  successes: number
  success_rate: number | null
  mean_sec: number | null
  median_sec: number | null
  graded: number
  correct: number
  accuracy: number | null
}

export interface Arm {
  label: string
  calls: number
  successes: number
  graded: number
  correct: number
  mean_sec: number | null
  median_sec: number | null
}

export interface Comparison {
  a: Arm
  b: Arm
  success_rate_a: number | null
  success_rate_b: number | null
  accuracy_a: number | null
  accuracy_b: number | null
  accuracy_diff: number | null
  accuracy_p_value: number | null
  speed_ratio: number | null
  warnings: string[]
}

export interface ImportResult {
  read: number
  inserted: number
  skipped: { line: number; reason: string }[]
}

export type Dimension = 'model' | 'provider' | 'task'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  })
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    throw new Error(body?.error ?? `HTTP ${res.status}`)
  }
  return body as T
}

export const api = {
  experiments: () => request<Experiment[]>('/api/experiments'),
  createExperiment: (e: ExperimentInput) =>
    request<Experiment>('/api/experiments', { method: 'POST', body: JSON.stringify(e) }),
  updateExperiment: (id: number, e: Omit<ExperimentInput, 'prediction'>) =>
    request<Experiment>(`/api/experiments/${id}`, { method: 'PUT', body: JSON.stringify(e) }),
  events: (limit = 200) => request<UsageEvent[]>(`/api/usage/events?limit=${limit}`),
  modelStats: () => request<ModelStat[]>('/api/usage/models'),
  importLog: () => request<ImportResult>('/api/usage/import', { method: 'POST' }),
  abOptions: (dim: Dimension) =>
    request<{ values: string[]; experiments: string[] }>(`/api/ab/options?dim=${dim}`),
  compare: (dim: Dimension, a: string, b: string, experiment: string) => {
    const q = new URLSearchParams({ dim, a, b, experiment })
    return request<Comparison>(`/api/ab?${q}`)
  },
}

export const pct = (v: number | null) => (v == null ? '—' : `${(v * 100).toFixed(1)}%`)
export const sec = (v: number | null) => (v == null ? '—' : `${v.toFixed(2)}s`)
