export const mockOutgoingRef = {
  target_entry_id: 'ent_002',
  target_ref: 'E2',
  entry_code: 'E2',
  entry_title: 'Reactive State Management',
  research_code: 'R1',
  research_name: 'Vue Component Architecture',
  resolved: true,
}

export const mockOutgoingRefCrossResearch = {
  target_entry_id: 'ent_r2_001',
  target_ref: 'R2:E1',
  entry_code: 'E1',
  entry_title: 'Pinia Store Patterns',
  research_code: 'R2',
  research_name: 'State Management Patterns',
  resolved: true,
}

export const mockOutgoingRefUnresolved = {
  target_entry_id: null,
  target_ref: 'E99',
  entry_code: null,
  entry_title: null,
  research_code: null,
  research_name: null,
  resolved: false,
}

export const mockIncomingRef = {
  source_id: 'ent_004',
  source_type: 'entry',
  entry_code: 'E4',
  entry_title: 'Slots and Render Functions',
  research_code: 'R1',
  research_name: 'Vue Component Architecture',
}

export const mockOutgoingRefs = [mockOutgoingRef, mockOutgoingRefCrossResearch, mockOutgoingRefUnresolved]
export const mockIncomingRefs = [mockIncomingRef]

/**
 * `GET /api/researches/{id}/crossrefs` summaries, as the settings card reads
 * them: how many references are indexed, how many resolve to nothing, and the
 * distinct codes behind them — already deduplicated and capped by the server,
 * with `dangling_total` saying how many distinct codes there really were.
 */
export interface CrossRefSummary {
  total: number
  unresolved: number
  dangling: string[]
  dangling_total?: number
}

/** Three codes point at nothing, out of forty-seven references. */
export const mockCrossRefSummaryBroken: CrossRefSummary = {
  total: 47,
  unresolved: 3,
  dangling: ['E20', 'E31', 'T7'],
  dangling_total: 3,
}

/** What a re-read returns after a rebuild that repaired two of the three. */
export const mockCrossRefSummaryStillBroken: CrossRefSummary = {
  total: 47,
  unresolved: 1,
  dangling: ['T7'],
  dangling_total: 1,
}

/** Everything resolves. */
export const mockCrossRefSummaryHealthy: CrossRefSummary = {
  total: 128,
  unresolved: 0,
  dangling: [],
  dangling_total: 0,
}

/** Nothing indexed at all — either a research that cites nothing, or a stale index. */
export const mockCrossRefSummaryEmpty: CrossRefSummary = {
  total: 0,
  unresolved: 0,
  dangling: [],
  dangling_total: 0,
}

/**
 * Three hundred broken references over forty distinct codes, of which the
 * server returns its cap of twelve. Session, question and cross-research
 * targets sit in the list beside entry and task codes, because the resolver has
 * never been able to resolve some of those and a rebuild will not change it.
 */
export const mockCrossRefSummaryOverloaded: CrossRefSummary = {
  total: 1240,
  unresolved: 300,
  dangling: ['E12', 'E19', 'E23', 'E30', 'E44', 'E51', 'E60', 'T7', 'T18', 'SS2', 'Q4', 'R2:E9'],
  dangling_total: 40,
}

/** Four hundred characters with no spaces — a thing a person can type between brackets. */
export const pathologicalRefCode =
  'FRONTEND-INGESTION-PIPELINE-RETRY-BUDGET-'.repeat(10).slice(0, 400)

export const mockCrossRefSummaryPathological: CrossRefSummary = {
  total: 9,
  unresolved: 1,
  dangling: [pathologicalRefCode],
  dangling_total: 1,
}

/** `POST /api/researches/{id}/crossrefs/rebuild` results. */
export const mockRebuildClean = { sources: 31, references: 47, unresolved: 0 }
export const mockRebuildStillBroken = { sources: 31, references: 47, unresolved: 1 }
