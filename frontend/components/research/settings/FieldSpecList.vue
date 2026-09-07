<template>
  <div class="spec-list">
    <div v-for="section in sections" :key="section.id" :id="`fields-${section.code || section.id}`" class="card spec-card">
      <div class="spec-head">
        <div class="spec-heading">
          <ShortCode v-if="section.code" :code="section.code" />
          <h3 class="card-title">{{ section.display_name || section.name }}</h3>
        </div>
        <div class="spec-head-right">
          <span class="cap-count" :class="{ 'is-over': countFor(section) >= caps.fields }">
            {{ countFor(section) }} / {{ caps.fields }}
          </span>
          <button
            v-if="onDelete"
            type="button"
            class="btn btn-sm"
            :class="{ 'btn-danger': !(section.entries_count > 0) }"
            :disabled="section.entries_count > 0 || busyDelete === section.id"
            :aria-describedby="section.entries_count > 0 ? `spec-refusal-${section.id}` : undefined"
            @click="askDelete(section)"
          >{{ busyDelete === section.id ? 'Deleting…' : 'Delete' }}</button>
        </div>
      </div>

      <!-- Visible, not a tooltip: a disabled control's `title` reaches neither a
           keyboard nor a screen reader, which is the rule DangerRow already
           states. And no "delete anyway" — the refusal *is* the feature; force
           exists for the API and MCP, where the caller is explicit by
           construction. -->
      <p v-if="onDelete && section.entries_count > 0" :id="`spec-refusal-${section.id}`" class="spec-refusal">
        Holds {{ section.entries_count }} {{ section.entries_count === 1 ? 'document' : 'documents' }}.
        Empty it first; documents cannot be moved between sections yet.
        <NuxtLink v-if="researchSlug" :to="`/research/${researchSlug}?section=${section.id}`" class="link-btn">Show them &rarr;</NuxtLink>
      </p>

      <p class="spec-blurb">
        <template v-if="countFor(section)">
          Documents in this section record these fields. Removing one never deletes what
          documents already carry under it.
        </template>
        <template v-else>
          This section declares nothing, so its documents carry no metadata. Most sections
          are topics rather than classes of document, and that is the normal case.
        </template>
      </p>

      <div v-if="drafts[section.id]" class="spec-editor">
        <p v-if="errors[section.id]" class="inline-error" role="alert">{{ errors[section.id] }}</p>

        <div v-for="(f, i) in drafts[section.id]" :key="i" class="spec-row">
          <input
            v-model="f.key"
            class="text-input spec-input spec-input--key"
            placeholder="key"
            :aria-label="`Field ${i + 1} key`"
          />
          <input
            v-model="f.label"
            class="text-input spec-input"
            placeholder="Label shown on the document"
            :aria-label="`Field ${i + 1} label`"
          />
          <select v-model="f.type" class="select spec-input spec-input--type" :aria-label="`Field ${i + 1} type`">
            <option v-for="t in types" :key="t.type" :value="t.type">{{ t.type }}</option>
          </select>
          <label class="spec-check">
            <input v-model="f.required" type="checkbox" />
            required
          </label>
          <label class="spec-check">
            <input v-model="f.repeated" type="checkbox" />
            list
          </label>
          <button class="btn btn-sm spec-remove" :aria-label="`Remove field ${i + 1}`" @click="removeField(section.id, i)">Remove</button>

          <input
            v-if="f.type === 'enum'"
            v-model="f.optionsRaw"
            class="text-input spec-input spec-input--wide"
            placeholder="options, comma, separated"
            :aria-label="`Field ${i + 1} options`"
          />
          <input
            v-model="f.help"
            class="text-input spec-input spec-input--wide"
            placeholder="Where does this value come from? The agent reads this."
            :aria-label="`Field ${i + 1} help`"
          />
        </div>

        <div class="spec-actions">
          <button
            class="btn btn-sm"
            :disabled="(drafts[section.id]?.length ?? 0) >= caps.fields"
            :title="(drafts[section.id]?.length ?? 0) >= caps.fields ? `A section may declare at most ${caps.fields} fields` : ''"
            @click="addField(section.id)"
          >Add field</button>
          <button class="btn btn-sm btn-primary" :disabled="busy === section.id" @click="save(section)">
            {{ busy === section.id ? 'Saving...' : 'Save' }}
          </button>
          <button class="btn btn-sm" :disabled="busy === section.id" @click="closeEditor(section.id)">Cancel</button>
        </div>
        <p class="spec-hint">
          Reserved: {{ reservedKeys.join(', ') }} &mdash; the export already emits those,
          and a second one would overwrite it.
        </p>
      </div>

      <template v-else>
        <ul v-if="countFor(section)" class="spec-fields">
          <li v-for="f in section.field_spec" :key="f.key" class="spec-field">
            <span class="spec-field-label">{{ f.label || f.key }}</span>
            <code class="spec-field-key">{{ f.key }}</code>
            <span class="spec-field-type">{{ f.type }}</span>
            <span v-if="f.required" class="spec-field-flag">required</span>
          </li>
        </ul>
        <div v-if="editable" class="spec-actions">
          <button class="btn btn-sm" @click="openEditor(section)">
            {{ countFor(section) ? 'Edit fields' : 'Declare fields' }}
          </button>
        </div>
      </template>

      <!--
        Under the field list, not above it. The instruction is supposed to name
        the keys this section declares, and putting it last is what keeps those
        keys on screen directly above the textarea somebody types the
        instruction into. That adjacency is the whole anti-drift argument.

        The child owns its own draft and error, so an open field-spec editor and
        an open instruction editor on the same card coexist without either
        knowing about the other.
      -->
      <ResearchSettingsInstructionEditor
        v-if="onSaveInstruction && (section.instruction || editable)"
        :instruction="section.instruction || ''"
        :field-keys="keysOf(section)"
        :editable="editable"
        :cap="caps.instruction_max ?? 500"
        :on-save="(instruction: string) => onSaveInstruction!(section.id, instruction)"
      />
    </div>

    <ConfirmModal
      :visible="!!pendingDelete"
      variant="danger"
      title="Delete section"
      :message="`“${pendingDelete?.display_name || pendingDelete?.name}” is empty, so nothing is filed under it. This cannot be undone.`"
      confirm-label="Delete"
      :loading="!!busyDelete"
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'

interface DraftField {
  key: string
  label: string
  type: string
  required: boolean
  repeated: boolean
  optionsRaw: string
  help: string
}

const props = defineProps<{
  sections: any[]
  editable?: boolean
  caps: { fields: number; required: number; options: number; instruction_max?: number }
  types: { type: string }[]
  reservedKeys: string[]
  /**
   * Persists a section's declaration and resolves when the server has agreed.
   *
   * A function prop, not an emit: `emit` returns undefined, so awaiting it
   * clears the busy state before the request lands and makes the error box
   * below unreachable — while the comment beside the call claims the opposite.
   */
  onSave?: (sectionId: string, spec: any[]) => Promise<void>
  /**
   * Persists a section's writing instruction. Optional: without it the row is
   * not rendered at all, which is what keeps this component usable in a story
   * that is only about field specs.
   */
  onSaveInstruction?: (sectionId: string, instruction: string) => Promise<void>
  /**
   * Deletes a section. Optional, and its absence removes the control entirely
   * rather than disabling it — the same rule the rest of the settings page
   * follows, and what keeps this component renderable in a story about field
   * specs alone.
   *
   * It is never called for a section holding documents: the button is disabled
   * and the reason is visible beside it. The API's `force` is deliberately
   * unreachable from here.
   */
  onDelete?: (sectionId: string) => Promise<void>
  /** For the "show them" link on a refusal. Omitted, the link is not rendered. */
  researchSlug?: string
}>()

const busyDelete = ref<string | null>(null)
const pendingDelete = ref<any | null>(null)

function askDelete(section: any) {
  if (section.entries_count > 0) return
  pendingDelete.value = section
}

async function confirmDelete() {
  const section = pendingDelete.value
  if (!section || !props.onDelete) return
  busyDelete.value = section.id
  try {
    await props.onDelete(section.id)
    pendingDelete.value = null
  } finally {
    busyDelete.value = null
  }
}

const drafts = reactive<Record<string, DraftField[] | undefined>>({})
const errors = reactive<Record<string, string>>({})
const busy = ref<string | null>(null)

function countFor(section: any): number {
  return section.field_spec?.length ?? 0
}

function keysOf(section: any): string[] {
  return (section.field_spec ?? []).map((f: any) => f.key).filter(Boolean)
}

function openEditor(section: any) {
  errors[section.id] = ''
  drafts[section.id] = (section.field_spec ?? []).map((f: any) => ({
    key: f.key ?? '',
    label: f.label ?? '',
    type: f.type ?? 'text',
    required: !!f.required,
    repeated: !!f.repeated,
    optionsRaw: (f.options ?? []).join(', '),
    help: f.help ?? '',
  }))
}

function closeEditor(sectionId: string) {
  drafts[sectionId] = undefined
  errors[sectionId] = ''
}

function addField(sectionId: string) {
  drafts[sectionId]?.push({
    key: '', label: '', type: 'text', required: false, repeated: false, optionsRaw: '', help: '',
  })
}

function removeField(sectionId: string, index: number) {
  drafts[sectionId]?.splice(index, 1)
}

async function save(section: any) {
  const draft = drafts[section.id]
  if (!draft) return
  busy.value = section.id
  errors[section.id] = ''
  try {
    const spec = draft.map(f => ({
      key: f.key.trim(),
      label: f.label.trim(),
      type: f.type,
      required: f.required,
      repeated: f.repeated,
      options: f.type === 'enum'
        ? f.optionsRaw.split(',').map(o => o.trim()).filter(Boolean)
        : undefined,
      help: f.help.trim(),
    }))
    // Waited, never optimistic: the server decides whether a key is reserved or
    // a cap is breached, and showing the new declaration before it agrees would
    // misstate what documents are being held to.
    await props.onSave?.(section.id, spec)
    closeEditor(section.id)
  } catch (e: any) {
    errors[section.id] = e?.data?.error || e?.message || 'Could not save'
  } finally {
    busy.value = null
  }
}
</script>

<style scoped>
.spec-list { display: flex; flex-direction: column; gap: var(--space-4); }
.spec-card { padding: var(--space-4); }

.spec-head {
  display: flex; align-items: center; justify-content: space-between;
  gap: var(--space-3); margin-bottom: var(--space-2);
}
.spec-heading { display: flex; align-items: center; gap: var(--space-2); min-width: 0; }
.spec-head-right { display: flex; align-items: center; gap: var(--space-3); flex-shrink: 0; }

/* Warning, not muted: this is the same concept as DangerRow's `.danger-reason`
   — a visible sentence explaining a disabled destructive control, wired through
   aria-describedby — and rendering one as a warning and the other as body
   chrome would make one idea look like two. */
.spec-refusal {
  margin: 0 0 var(--space-2);
  font-size: var(--type-xs);
  color: var(--color-warning);
}

.spec-blurb {
  margin: 0 0 var(--space-3);
  font-size: var(--type-xs);
  color: var(--color-text-muted);
}

.spec-fields { list-style: none; margin: 0 0 var(--space-3); padding: 0; }
.spec-field {
  display: flex; align-items: center; gap: var(--space-2);
  padding: var(--space-2) 0;
  border-top: 1px solid var(--color-border);
  font-size: var(--type-sm);
}
.spec-field-label { font-weight: var(--weight-semibold); }
.spec-field-key { font-size: var(--type-xs); color: var(--color-text-faint); }
.spec-field-type { font-size: var(--type-xs); color: var(--color-text-muted); }
.spec-field-flag { font-size: var(--type-xs); color: var(--color-warning); }

.spec-editor { display: flex; flex-direction: column; gap: var(--space-3); }
.spec-row {
  display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2);
  padding-top: var(--space-2);
  border-top: 1px solid var(--color-border);
}

/* Stated so a text input, a select and a button in one row come out level
   rather than at three heights derived from three paddings. */
.spec-input { height: var(--control-h); min-width: 0; }
.spec-input--key { flex: 0 1 9rem; }
.spec-input--type { flex: 0 1 8rem; }
.spec-input--wide { flex: 1 1 100%; }
.spec-input:not(.spec-input--key):not(.spec-input--type):not(.spec-input--wide) { flex: 1 1 12rem; }

.spec-check {
  display: inline-flex; align-items: center; gap: 0.25rem;
  height: var(--control-h);
  font-size: var(--type-xs); color: var(--color-text-muted);
  white-space: nowrap;
}
/* Stated, like every other control in the row. `.btn.btn-sm` comes out at
   26.6px from its own padding, and a row of 30, 30, 30, 30, 30 and 26.6 is the
   difference that is visible and hard to name. */
.spec-remove { flex-shrink: 0; height: var(--control-h); }

.spec-actions { display: flex; gap: var(--space-2); flex-wrap: wrap; }

.spec-hint { margin: 0; font-size: var(--type-xs); color: var(--color-text-faint); }
</style>
