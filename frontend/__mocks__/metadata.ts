/**
 * What a section declares, and what its documents actually recorded.
 *
 * Three surfaces read the same two shapes — the metadata block on a document,
 * the section table, and the field editor in settings — so they are written
 * once here. A story that invents its own spec is how the block and the table
 * end up demonstrating different rules.
 *
 * The corpus is the one from issue #79 rather than the catalogue's usual Vue
 * material: a section of eighteen sibling specifications, in Russian, where the
 * same five facts were being typed into the first five lines of prose. Two
 * things only show up in that shape — a long Cyrillic label against
 * `--metadata-col-max`, and a field that every document leaves blank.
 */
import type { FieldSpec } from '~/composables/useFieldSpec'

/**
 * The declaration on "Спецификации".
 *
 * `stage` is an enum on purpose and the issue's whole argument for types is in
 * it: narrowing the answer space is what raises fill rate, while a typed date
 * fills no better than an untyped one. It sits beside the system `status`,
 * which is the pair the block is built to show — they are the two that
 * disagreed in prose.
 */
export const specSpecifications: FieldSpec[] = [
  { key: 'stage', label: 'Стадия', type: 'enum', options: ['draft', 'in-review', 'agreed', 'superseded'] },
  { key: 'produces', label: 'Produces', type: 'text', repeated: true },
  { key: 'consumes', label: 'Consumes', type: 'text', repeated: true },
  {
    key: 'owner',
    label: 'Owner',
    type: 'text',
    required: true,
    help: 'The service name from the repo. Ask rather than guess.',
  },
  { key: 'registry', label: 'Registry', type: 'ref' },
  { key: 'reviewed', label: 'Reviewed', type: 'date' },
]

/**
 * One declared field. The common shape after somebody's first edit, and the
 * case the table deliberately refuses to offer a toggle for.
 */
export const specSingleField: FieldSpec[] = [
  { key: 'stage', label: 'Стадия', type: 'enum', options: ['draft', 'in-review', 'agreed'] },
]

/**
 * Twelve fields — `MaxSectionFields` — with five required, which is the second
 * and harder cap (`MaxRequiredFields`). They are independent, so a section can
 * sit on both at once and this is what that looks like.
 */
export const specAtCap: FieldSpec[] = [
  ...specSpecifications,
  { key: 'component', label: 'Component', type: 'text', required: true, help: 'The deployable this spec belongs to.' },
  { key: 'transport', label: 'Transport', type: 'enum', options: ['http', 'grpc', 'queue', 'temporal'], required: true },
  { key: 'schema_url', label: 'Schema', type: 'url' },
  { key: 'retries', label: 'Retries', type: 'number' },
  { key: 'supersedes', label: 'Supersedes', type: 'ref', repeated: true },
  { key: 'reviewer', label: 'Reviewer', type: 'text', required: true, help: 'Whoever signed off. Ask if unclear.' },
]

/**
 * Labels and values as this product actually receives them. `--metadata-col-max:
 * 16rem` is the only thing between the first label here and a value column
 * pushed off the page.
 */
export const specCyrillic: FieldSpec[] = [
  { key: 'stage', label: 'Стадия согласования спецификации площадки', type: 'text' },
  { key: 'produces', label: 'Кто формирует полезную нагрузку', type: 'text', repeated: true },
  {
    key: 'owner',
    label: 'Ответственная команда сопровождения',
    type: 'text',
    required: true,
    help: 'Название сервиса из репозитория. Спросите, если неясно, а не угадывайте.',
  },
]

/** Every declared field answered. */
export const metadataFilled: Record<string, unknown> = {
  stage: 'in-review',
  produces: ['scanner-watchdog'],
  consumes: ['scanner-orchestrator'],
  owner: 'platform',
  registry: 'E47',
  reviewed: '2026-08-14',
}

/** Two answered, the required one not. The ordinary in-progress document. */
export const metadataPartial: Record<string, unknown> = {
  stage: 'draft',
  produces: ['scanner-watchdog'],
}

/**
 * Nothing answered at all — a document created before the section declared
 * anything, or written by an agent that ignored the schema. Every row is the
 * blank cell the feature exists to make visible.
 */
export const metadataNothingFilled: Record<string, unknown> = {}

/**
 * The explicit unknown, which is `null` and not absence. It answers a required
 * field without inventing a fact, which is the counterweight to an author that
 * never leaves a blank.
 */
export const metadataUnknownOwner: Record<string, unknown> = {
  ...metadataFilled,
  owner: null,
}

/** A stored value that no longer matches its declaration. Kept, flagged, re-checked on read. */
export const metadataInvalidStage: Record<string, unknown> = {
  ...metadataFilled,
  stage: 'sent for review',
}

/**
 * Values under keys nothing declares any more — the section was rewritten and
 * every old key was dropped. Removing a field decides what gets collected next;
 * it is not a verdict on what was already recorded, so all of this still shows.
 */
export const metadataAllOrphaned: Record<string, unknown> = {
  implemented_by: 'scanner-watchdog, scanner-orchestrator',
  related: 'SPEC-02, SPEC-03, SPEC-04',
  reviewed_on: '14.08.2026',
  status_note: 'черновик на ревью',
}

/** Cyrillic values against the Cyrillic labels. */
export const metadataCyrillic: Record<string, unknown> = {
  stage: 'черновик, отправленный на ревью команде платформы и оркестратора',
  produces: ['scanner-watchdog', 'scanner-orchestrator', 'cluster-watchdog'],
  owner: 'команда платформы наблюдаемости площадок',
}

/**
 * The shape a section instruction is supposed to have: three to six
 * imperatives, naming the section's own declared fields.
 *
 * Not invented — this is the convention the eighteen hand-written SPEC
 * preambles in R21 were all groping towards, written once above the documents
 * instead of eighteen times inside them.
 */
export const mockSectionInstruction =
  'Назови производящий сервис в поле service.\n' +
  'Назови потребителя в поле consumer.\n' +
  'Один абзац обоснования, затем полезная нагрузка.\n' +
  'Заполни owner и status до сохранения.'

/**
 * The section itself, shaped as the research payload delivers it — `field_spec`,
 * `spec_version` and `instruction` beside the ordinary section fields.
 *
 * It carries an instruction, so every story that hands this to `EntriesView`
 * shows the read block above the documents as well as the field chips.
 */
export const mockSpecSection = {
  id: 'sec_spec',
  code: 'S13',
  name: 'specifications',
  display_name: 'Спецификации',
  description: 'Одна спецификация на блок реестра.',
  status: 'active',
  entries_count: 6,
  field_spec: specSpecifications,
  spec_version: 4,
  instruction: mockSectionInstruction,
}

/** A section that declares nothing and says nothing about how to write in it:
 *  a topic, not a class of document — which is most sections. */
export const mockTopicSection = {
  id: 'sec_questions',
  code: 'S11',
  name: 'open-questions',
  display_name: 'Вопросы на повестку',
  description: null,
  status: 'active',
  entries_count: 8,
  field_spec: [],
  spec_version: 0,
  instruction: '',
}

/**
 * Six sibling specifications for the table.
 *
 * `reviewed` is empty on every one of them, deliberately. That column is the
 * output the feature exists to produce: a field nobody fills is not noise, it
 * is the answer to whether the field should have been declared — and it is only
 * legible as a column of dashes standing next to filled ones.
 */
export const mockSpecEntries = [
  {
    id: 'ent_050', code: 'E50', section_id: 'sec_spec',
    title: 'SPEC-01 · Payload состояния площадки',
    description: 'Первая спецификация блока 1 по реестру [[E47]].',
    status: 'active', tags: ['spec', 'payload'], spec_version: 4,
    metadata: { stage: 'in-review', produces: ['scanner-watchdog'], consumes: ['scanner-orchestrator'], owner: 'platform', registry: 'E47' },
  },
  {
    id: 'ent_051', code: 'E51', section_id: 'sec_spec',
    title: 'SPEC-02 · Пробы площадки и вердикт о её работоспособности',
    description: 'Что считается пробой и кто выносит вердикт.',
    status: 'active', tags: ['spec', 'probes'], spec_version: 4,
    metadata: { stage: 'agreed', produces: ['scanner-probe'], consumes: ['scanner-orchestrator', 'temporal-watchdog'], owner: 'platform', registry: 'E47' },
  },
  {
    id: 'ent_052', code: 'E52', section_id: 'sec_spec',
    title: 'SPEC-03 · Сервис watchdog',
    description: 'Границы ответственности сторожа.',
    status: 'completed', tags: ['spec'], spec_version: 4,
    // The required field unanswered: one "1 missing" chip, one blank cell.
    metadata: { stage: 'agreed', produces: ['scanner-watchdog'] },
  },
  {
    id: 'ent_053', code: 'E53', section_id: 'sec_spec',
    title: 'SPEC-04 · platform-profile',
    description: 'Профиль площадки как единица конфигурации.',
    status: 'draft', tags: ['spec', 'config'], spec_version: 3,
    // Explicit unknown: the table prints "unknown", not a dash, and the chip
    // stays away — somebody looked, which is a different fact from silence.
    metadata: { stage: 'draft', owner: null, registry: 'E47' },
  },
  {
    id: 'ent_054', code: 'E54', section_id: 'sec_spec',
    title: 'SPEC-05 · Инциденты без права решения',
    description: 'Эмиттер, который не принимает решений.',
    status: 'draft', tags: ['spec'], spec_version: 4,
    // Nothing at all. Written before the section declared anything.
    metadata: {},
  },
  {
    id: 'ent_055', code: 'E55', section_id: 'sec_spec',
    title: 'SPEC-06 · Реестр блоков и его владельцы',
    description: 'Кто ведёт реестр и по каким правилам.',
    status: 'active', tags: ['spec', 'registry'], spec_version: 4,
    metadata: {
      stage: 'superseded',
      produces: ['registry-api', 'registry-worker', 'registry-cli', 'registry-exporter'],
      consumes: ['scanner-orchestrator'],
      owner: 'команда платформы наблюдаемости площадок',
      registry: 'E47',
    },
  },
]

/**
 * The rules, as `GET /api/metadata/schema` returns them.
 *
 * Copied from `domain.FieldSchema()` rather than invented: the editor reads
 * them from the server precisely so a cap it believes and a cap the server
 * enforces cannot disagree, and a mock that made up its own numbers would put
 * the disagreement back into the catalogue.
 */
export const fieldCaps = {
  fields: 12,
  required: 5,
  options: 20,
  values: 20,
  text_max: 200,
  label_max: 60,
  help_max: 200,
  key_max: 32,
  key_pattern: '^[a-z][a-z0-9_]*$',
  instruction_max: 500,
}

export const fieldTypes = [
  { type: 'enum', needs_option: true, repeatable: true, hint: 'A short list of named options.' },
  { type: 'ref', repeatable: true, hint: 'A short code of something in this product: E12, R2:E5, RM1.' },
  { type: 'date', repeatable: false, hint: 'A calendar date, YYYY-MM-DD.' },
  { type: 'text', repeatable: true, hint: 'One short line.' },
  { type: 'number', repeatable: false, hint: 'A number.' },
  { type: 'url', repeatable: true, hint: 'An http or https address.' },
]

/** The keys the Obsidian export already emits. Sorted, as the server sorts them. */
export const reservedKeys = [
  'aliases', 'code', 'created', 'research', 'section',
  'session', 'status', 'tags', 'title', 'type', 'updated',
]
