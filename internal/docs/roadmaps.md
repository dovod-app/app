# Roadmaps

Roadmaps are visual directed graphs within a research — learning paths, strategy maps, decision trees, or step-by-step guides. Unlike the auto-generated mindmap (which visualizes all research data), roadmaps are deliberately designed and their nodes can track progress with custom statuses.

The same nodes can be laid out three ways, switchable from a toggle in the UI:

- **Graph** — the free node-edge graph (the default).
- **Stages** — nodes grouped into ordered phase columns by `node.stage`, kanban-style.
- **Timeline** — nodes placed on a month axis by `node.node_date`.

Set `view` on the roadmap to choose which one it opens in; set `stages` (an ordered list of column names) and put a node in a column with `stage`; give a node a `node_date` (ISO `YYYY-MM-DD`) to place it on the timeline. These are all optional — a roadmap with none of them is a graph exactly as before. Dependency edges are not drawn in the stages and timeline views (columns and cells scroll independently); each card instead lists the predecessors it depends on. The view is a display choice, not stored per node — the graph, the columns and the timeline are three renderings of one set of nodes and edges.

## MCP Tools

| Tool | Description |
|------|-------------|
| `roadmap_create` | Create a complete roadmap with nodes and edges in one call |
| `roadmap_get` | Get a roadmap with all its nodes and edges |
| `roadmap_list` | List roadmaps for a research (metadata only) |
| `roadmap_update` | Update roadmap title, description, statuses, stages, view, or status |
| `roadmap_delete` | Delete a roadmap and all its nodes/edges |
| `roadmap_add_nodes` | Add new nodes and edges to an existing roadmap |
| `roadmap_update_node` | Update a single node (status, title, description, type, position, stage, node_date) |
| `roadmap_remove_nodes` | Remove nodes by ID (connected edges auto-delete) |

## When to Create a Roadmap

| Situation | Example |
|-----------|---------|
| Topic has a clear learning sequence | "Vue 3 Learning Path": HTML → JS → Vue basics → Composition API → Testing → Deploy |
| Research uncovers a multi-step process | "Database Migration Plan": backup → schema changes → data migration → validation → cutover |
| User asks "how do I get from A to B?" | "Career Path to Senior Engineer": skills → projects → mentoring → leadership |
| Multiple alternatives exist | "Frontend Framework Decision": evaluate → benchmark → decide (React / Vue / Svelte branches) |
| System architecture mapping | "Microservices Dependencies": API Gateway → Auth → Users → Orders → Payments |
| Onboarding or process flow | "New Hire Onboarding": paperwork → tooling → codebase → first PR → first feature |

## When NOT to Use a Roadmap

Use sections and entries instead when:
- Content is purely textual with no inherent sequence
- A simple list or table would suffice
- The information doesn't have meaningful relationships between items
- You just need to organize findings by topic (that's what sections are for)

## Node Types

| Type | Purpose | Rendering |
|------|---------|-----------|
| `step` | Regular action item or learning step (default) | Green tint + icon badge |
| `milestone` | Key achievement or checkpoint | Purple tint + icon badge |
| `decision` | Fork in the path where a choice is needed | Amber tint + icon badge |
| `info` | Reference material, prerequisite, or note | Blue tint + icon badge |
| `group` | Container for related steps (visual grouping) | Gray tint + icon badge |
| `checklist` | List of sub-items — put the items in `metadata` | Plain node (no dedicated styling yet) |
| `note` | Free-form annotation | Plain node |
| `link` | External URL reference — put the URL in `metadata` | Plain node |
| `metric` | KPI or numeric indicator — put the value in `metadata` | Plain node |

`node_type` is a free-form string in storage; these nine are the values the tools document and the UI knows about. `metadata` is a JSON string, stored verbatim and returned as-is.

## Views: stages and timeline

Three fields turn a plain graph into a staged board or a dated timeline. All are optional and default to the graph.

| Field | On | Meaning |
|-------|----|---------|
| `view` | roadmap | `graph` (default) / `stages` / `timeline` — the layout it opens in. The UI toggle overrides this locally; it is the default, not a lock. |
| `stages` | roadmap | Ordered list of column names for the stages view, e.g. `["Discovery","Design","Build","Launch"]`. A name here with no nodes is a legitimately empty column — the ordering is the column order. Relates to a node's `stage` exactly as `statuses` relates to a node's `status`. |
| `stage` | node | Which stage column the node sits in. Matched by string against the roadmap's `stages`; a value that is empty or not in the list falls into a trailing **Unassigned** column rather than erroring. |
| `node_date` | node | ISO `YYYY-MM-DD` (or empty). The node's point — or the **start** of a range — on the timeline. Empty means undated (set aside in a tray). A `milestone` with a date is a diamond marker. |
| `node_end_date` | node | Optional ISO `YYYY-MM-DD`. With `node_date` set, the node renders as a **bar** from start to end (a Gantt bar); empty means a point. Rejected with `400` if it precedes `node_date`. Ignored for a `milestone` (a milestone is an instant). |

`view` is validated (one of the three); `node_date` and `node_end_date` are validated (strict `YYYY-MM-DD`, or empty), and an end before the start is a `400` on `node_end_date`. `stage` is free-form like `status` — an unknown stage is tolerated, not rejected.

The timeline reads durations, not only points: a node with `node_date` + `node_end_date` is a bar spanning its months, greedily laned so overlaps don't stack; a node with only `node_date` stays a point. A local **Month / Quarter / Year** zoom control compresses a multi-year plan (the opening zoom is picked from the span), and it is display-only — no data, nothing stored.

```javascript
roadmap_create({
  research_id: "<uuid>",
  title: "Launch plan",
  view: "stages",
  stages: ["Discovery", "Design", "Build", "Launch"],
  statuses: ["todo", "doing", "done"],
  nodes: [
    { temp_id: "n1", title: "Market research", stage: "Discovery", node_date: "2026-01-15", status: "done" },
    { temp_id: "n2", title: "Spec",            stage: "Design",    node_date: "2026-02-10" },
    { temp_id: "n3", title: "GA", node_type: "milestone", stage: "Launch", node_date: "2026-04-01" }
  ],
  edges: [{ source: "n1", target: "n2" }]
})
```

## Entity References (ref_type + ref_id)

Nodes can link to existing research entities. When a node has `ref_type` and `ref_id`, the roadmap displays live data from the referenced entity (title, status, progress). This enables roadmaps that act as dashboards over research content.

| ref_type | References | Synced fields |
|----------|-----------|---------------|
| `entry` | An entry in any research | Title, status, content preview, section name |
| `task` | A task in the research | Title, status, priority, result |
| `session` | An interview session | Title, status, question progress (X/Y answered) |
| `research` | Another research | Name, status, section count, entry count |
| `question` | A question in a session | Text, status, answer |

Referenced data is resolved at read time (lazy sync) — always shows the current state of the entity. The entry preview is the first 200 characters of the content; for a [block document](/llms/blocks.md) it is that document rendered to markdown rather than the stored JSON, and an `html` block is named there rather than inlined.

### Creating Reference Nodes

The examples in this guide show only the fields that carry information. Real calls must include every property of the tool and of each node/edge object — send `null` for the ones you are not setting. See [MCP Client Guide](/llms/mcp-client-guide.md) → Nullable and Optional Fields.

```
roadmap_create({
  research_id: "...",
  title: "Project Dashboard",
  statuses: ["todo", "in_progress", "done"],
  nodes: [
    { temp_id: "n1", title: "Analyze competitors", node_type: "step", ref_type: "task", ref_id: "<task-uuid>" },
    { temp_id: "n2", title: "Architecture doc", node_type: "step", ref_type: "entry", ref_id: "<entry-uuid>" },
    { temp_id: "n3", title: "User interviews", node_type: "step", ref_type: "session", ref_id: "<session-uuid>" },
    { temp_id: "n4", title: "Related research", node_type: "info", ref_type: "research", ref_id: "<research-uuid>" },
  ],
  edges: [
    { source: "n1", target: "n2" },
    { source: "n2", target: "n3" },
    { source: "n3", target: "n4" },
  ]
})
```

## Edge Types

| Type | Purpose | Example label |
|------|---------|---------------|
| `default` | Normal progression | "next", "then", "requires" |
| `success` | Positive outcome path | "if passed", "on success", "approved" |
| `warning` | Failure or risk path | "if failed", "if blocked", "rejected" |
| `optional` | Non-required alternative | "alternative", "skip", "advanced only" |

## Custom Statuses

Each roadmap defines its own status vocabulary in the `statuses` field. This allows the same system to model different domains:

| Domain | Statuses |
|--------|----------|
| Learning path | `["not_started", "learning", "practiced", "mastered"]` |
| Marketing strategy | `["planned", "approved", "launched"]` |
| Engineering plan | `["todo", "in_progress", "review", "done"]` |
| Hiring pipeline | `["open", "screening", "interview", "offer", "hired"]` |
| No tracking | `[]` (purely structural graph, nodes have no status) |

Node statuses are free-form strings but should match the roadmap's `statuses` list for consistent UI rendering (colors, progress bars).

## How to Build a Roadmap

### Step 1: Create the full graph in one call

Use `roadmap_create` with all nodes and edges. Assign `temp_id` to each node so edges can reference them before real IDs exist.

```
roadmap_create({
  research_id: "...",
  title: "Vue 3 Learning Path",
  statuses: ["not_started", "in_progress", "completed"],
  nodes: [
    { temp_id: "n1", title: "HTML & CSS Basics", node_type: "step", status: "completed" },
    { temp_id: "n2", title: "JavaScript ES6+", node_type: "step", status: "in_progress" },
    { temp_id: "n3", title: "Fundamentals Complete", node_type: "milestone", status: "not_started" },
    { temp_id: "n4", title: "Choose framework path", node_type: "decision", status: "not_started" },
  ],
  edges: [
    { source: "n1", target: "n2", label: "next" },
    { source: "n2", target: "n3", label: "next" },
    { source: "n3", target: "n4", label: "next" },
  ]
})
```

### Step 2: Extend as needed

Use `roadmap_add_nodes` to add new nodes and edges to the graph. New edges can reference both `temp_id` of new nodes and real IDs of existing nodes.

### Step 3: Track progress

Use `roadmap_update_node` to change node statuses as the user progresses through the roadmap.

### Step 4: Prune

Use `roadmap_remove_nodes` to remove nodes that are no longer relevant. Edges connected to removed nodes are deleted automatically. Every id must belong to the roadmap you name: an id from another roadmap reads as `not found`, and in that case nothing in the list is removed. The same rule holds for every node reference you write — an edge's `source` and `target` and a node's `parent_id` may name a `temp_id` of the same request or a node already in that roadmap, and a request that names anything else creates nothing.

## Best Practices

- **Build the full graph in one call** — `roadmap_create` with all nodes and edges is better than adding them one by one
- **Create roadmaps after gathering enough information** — typically after 1-2 research sessions when the structure is clear
- **Use milestone nodes** to mark key achievements or checkpoints that the user can celebrate
- **Use decision nodes** when the path genuinely branches — don't force a linear sequence when alternatives exist
- **Use info nodes** for prerequisites or context that isn't an action step
- **Keep node descriptions concise** — detailed content belongs in entries, roadmap nodes provide the overview
- **Choose domain-appropriate statuses** — "mastered" feels right for learning, "launched" for marketing, "deployed" for engineering
- **Leave statuses empty** for purely structural graphs (architecture diagrams, dependency maps) where progress tracking doesn't apply
