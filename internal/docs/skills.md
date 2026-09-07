# Skills

A skill is a methodology document the agent opens when it decides it needs it.

> **Goal and description explain this research. Private skills carry its
> specific rules; team and built-in skills describe reusable ways of working.
> A [template](/llms/templates.md) says how a kind of research is *started*.**

`research_get` lists the skills a research follows — name, tier and one line
saying when to use it. The bodies are not there. When you are about to do the
work a line names, call `skill_load` and read it then.

## Why it works this way

`research_get` returns structured memory and the skills index, without skill
bodies. A methodology — how to run an interview, how to grade a source, how to
build a trade-off table — is needed at the moment of use. Loading every body
on every call wastes context. Research-specific rules use the same private
skill mechanism instead of an always-loaded instruction field.

Legacy `instruction` text is preserved in an attached private skill marked
`needs_trigger`. Review its body and give it a concrete trigger description
with `skill_update` so future sessions know when to load it.

So the index is small and arrives with the research, and the bodies are one call
away.

## Reading them

```
research_get(research_id) -> {
  research: {...},
  sections: [...],
  active_session: {...},
  skills: [
    {slug: "evidence-grading", name: "Grading evidence", tier: "team",
     description: "Use when recording a claim that rests on a source..."},
    ...
  ],
  skills_hint: "Each skill says when to use it. Call skill_load ..."
}

skill_load(research_id, slug) -> {slug, name, tier, description, body,
                                  version, updated_at, precedence}
```

Four fields per entry, never a body. Because the product skills are always in it,
the index is never empty in a working install — so a missing `skills` key means
the built-ins failed to load, not that this research has none.

`skill_load` takes the research **UUID or the `R1` short code** — whichever you
have. `version` and `updated_at` come back with the body, so an agent holding an
older copy can tell it has moved on.

Two rules the tool enforces rather than suggests:

- **One slug per call.** There is no batch form. Loading everything up front is
  the thing this design exists to prevent.
- **Load at the point of use**, not while orienting. A skill read three steps
  before the work it describes has usually been forgotten by the time it matters.

`skill_load` is the only tool that returns a body. Everything else a skill tool
does is listed under [Managing them](#managing-them) — the same acts the web UI
performs, over the same service.

## Managing them

Ten tools, and only one of them returns a body.

| Tool | Does |
|---|---|
| `skill_load(research_id + slug \| skill_id)` | the full text of one skill. The only tool that returns a body |
| `skill_list(research_id \| team_id, query?)` | with `research_id`: `following`, `available`, `chosen`, `cap` — and `cap_reached: true` with a `cap_hint` when the budget is spent. The call to make before changing anything: what is on, what could be, how much is left. `query` filters `available` by name or trigger line. With `team_id` instead: that team's whole `library`, including skills no research follows yet, and no budget — the cap belongs to a research |
| `skill_attach(research_id, slug)` | make this research follow an existing skill. Attaching one it already follows is `already_attached`, not an error to work around |
| `skill_detach(research_id, slug)` | stop following it. A research-private skill is **deleted** by this, and the answer says so: `deleted: true` with the tier and name of what went |
| `skill_create(research_id \| team_id, name, description, body)` | write one. `research_id` makes it research-private and attaches it (spending a slot); `team_id` — the id from `team_list` — puts it in the library, attached to nothing, and the answer says to attach it separately. Exactly one of the two |
| `skill_update(research_id + slug \| skill_id, name?, description?, body?)` | edit in place. Omitted fields are inherited, so fixing a trigger line does not mean resending the body. Send at least one; all three omitted is refused. A built-in is `not_allowed` and the message names `skill_fork` |
| `skill_fork(research_id, slug, name?, description?, body?)` | **built-in only** → an editable team copy keeping the same slug, attachment moved in one step. Every field may be omitted: a bare fork copies the built-in as it is, to edit later. A team or private slug here is `not found` — for those, edit it or `skill_copy` it |
| `skill_copy(research_id, slug)` | team or built-in → a research-private copy, attachment moved in one step. A slug that is already private answers `copied: false` and changes nothing |
| `skill_promote(research_id, slug)` | research-private → the team library, attachment follows, the private original is deleted. Anything that is not private is `not_allowed` |
| `skill_delete(research_id + slug \| skill_id)` | remove a team or private skill from existence — as opposed to `skill_detach`, which removes it from one research. A team skill any research still follows is `skill_in_use`; a built-in is `not_allowed` |

Every one of them takes the research **UUID or the `R1` short code**.

Three of them are addressed twice over, by `research_id` + `slug` or by
`skill_id`, and that is not redundancy: an agent working inside a research holds
slugs and nothing else, while one that has just written a team skill holds the
id it was handed back and has no research to look it up through. Give one form
or the other, never both.

Addressed by **slug**, a miss says so and says why: `not_found` with the slug
quoted and a pointer to `skill_list`. Addressed by **id**, a skill that does not
exist and one that belongs to somebody else are deliberately the same refusal,
and it invites you nowhere.

**A refusal leads with its code**, the same vocabulary the REST API answers with
and from the same switch — `skill_cap_reached: this research already follows…`.
Read the code, not the sentence: `skill_cap_reached` means drop something first,
`already_attached` means carry on, and prose alone does not separate them.

| Code | What to do |
|---|---|
| `skill_cap_reached` | Six chosen skills are already on. Detach one before attaching or writing another — and not a product skill, which is outside the budget and cannot be dropped anyway. Retrying unchanged never succeeds |
| `already_attached` | Nothing to do: the research follows it already. Carry on |
| `slug_taken` | Something in the same tier scope holds that slug. **Renaming does not help** — see below. Find that row and edit it with `skill_update`; for the `skill_promote` case it is a team skill, so `skill_list(team_id)` gives you its `skill_id`. Do **not** reach for `skill_delete(research_id, slug)` there: a slug resolves to what the research follows first, which is the private copy you were trying to save |
| `skill_in_use` | A team skill other researches still follow; the count is in the message. Detach it from them, or leave it and detach it here instead |
| `not_allowed` | The act is refused by kind, not by state: detaching a product skill, writing to a built-in, promoting something that is not private. There is no retry — the message names the tool that does work |

**A slug is fixed at creation.** It is derived from `name` when the skill is
written and nothing changes it afterwards: `skill_update` renames the skill and
leaves the slug alone, and a fork or a copy keeps the slug of what it came from.
So `slug_taken` is never cleared by choosing a different name — it is cleared by
dealing with the row already holding the slug. `skill_promote` hits this most
often, when the team library already holds a fork of the same built-in the
private copy came from.

**The cap counts what somebody chose.** Six per research, and writing a
research-private skill spends a slot because it is attached the moment it exists
— so the seventh `skill_create` is refused exactly like the seventh
`skill_attach`. `skill_fork` and `skill_copy` move an existing attachment and
cost nothing extra, unless the source was in the library and not yet attached
here, in which case they take a slot and can be refused for it too.

Nothing here writes at the built-in tier. Those rows are rewritten from the
binary on every boot, so an edit there would be destroyed by the next upgrade —
which is why editing one is `skill_fork` and why `skill_delete` refuses.

## Tiers, and what wins

| Tier | Owner | Notes |
|---|---|---|
| `private` | one research | Where a rule that applies only here lives. Never offered to another research. |
| `team` | a team | Reusable across that team's researches. |
| `builtin` | nobody | Ships with the binary, refreshed on upgrade. Editing one forks a team copy; the original is never changed. |

**The index is ordered by tier, and that order is the precedence.** Where two
skills conflict, the higher one wins: private over team, team over built-in.
`skill_load` restates this in every response, because it is the only rule
governing which of two skills the agent follows. Private skills therefore
take precedence for research-specific working rules.

**A slug resolves against the attachment first.** A team's fork keeps the slug of
the built-in it copies, so a research that follows the built-in and never took
the fork still gets the built-in body: what the research actually follows is
looked up before what it merely could attach. Only when a slug is attached to
nothing here does the wider scope (this research's private skills, its team's
library, the built-ins) answer, highest tier first.

## Product skills are not counted

Some built-ins describe the product rather than a domain — how to manage a
research, how to write entries, what an artifact is, how roadmaps work. Four of
the six shipped skills are marked `ambient`: once attached they are **never
counted against the cap** and **cannot be detached** (`not_allowed`, 403). The
flag is structural, set from the shipped file at boot, and never editable through
the API.

**They need no attaching.** The always-on set is unioned into every index and
every attached list, so a research nobody has curated still gets them — and the
agent still learns from the first `research_get` that skills exist. Their bodies
load without an attachment too. Everything else does have to be attached, either
by a person, by `skill_attach`, or by `research_create` with a `template_slug`,
which attaches the skills that [template](/llms/templates.md) names at creation
and once. Rows attached that last way carry `via_template: true` in the attached
listing, so a reader can tell a methodology's choice from a person's.

Everything non-ambient counts. A research may follow **six** chosen skills; the
seventh is refused with `skill_cap_reached`, and so is writing a seventh private
one, because a private skill is attached the moment it is created. The limit is a
budget over what somebody decided to follow, not over what the product needs to
function.

### What ships

Six built-ins, refreshed from the binary on every boot — an upgrade rewrites the
shipped rows and never touches a team's fork of one. A built-in that disappears
from a later build is left in the database rather than deleted: a research may
still be following it.

| Slug | Ambient |
|---|---|
| `managing-a-research` | yes |
| `writing-entries` | yes |
| `writing-artifacts` | yes |
| `building-roadmaps` | yes |
| `evidence-grading` | no |
| `structured-interviewing` | no |

## Writing one

Three fields matter, and one of them decides whether the skill is ever used.

**`description` is a trigger, not a summary.** It is the only line always in the
agent's context, capped at 200 characters — counted in runes, so a Cyrillic
trigger line is not worth half a Latin one — and the agent matches situations
rather than topics. Write *"Use when comparing two or more suppliers against a
fixed rubric"*, not *"A guide to vendor scoring"*. A description that describes
gets a skill nobody opens. Over the cap is a field error, never a silent
truncation: a truncated trigger line is a skill that stops working.

**`body`** is markdown, 600–2500 tokens, and required. Over 16000 characters
(runes again) is refused: at that size it is two skills.

**`name`** is required and derives the slug. Uniqueness is per tier *scope* —
globally for built-ins, per team, per research — so a built-in and a team's fork
of it deliberately share a slug, while a second fork of the same built-in in the
same team is `slug_taken`. A name with no Latin characters gets a generated
`skill-xxxxxxxx` slug rather than failing to save.

## What a skill is not

- **Not a starting kit.** A skill is read at the moment of the work it describes,
  not once before a research exists to say how to *start* one. That is a
  [template](/llms/templates.md): read once at kickoff, in full, by a model that
  has nothing yet. A template may **name** skills, and `research_create` with a
  `template_slug` attaches them — which is the one place the two meet. Everything
  else about them is separate: a template is chosen and then done, a skill is
  opened again and again for as long as the research runs.
- **Not a place for rows.** A skill creates no sections, no questions, no tasks.
  It changes how you do the work; it never stands in for doing it.
- **Not tone or format policy for one research.** That belongs in the research
  itself; a skill that legislates entry length or house voice is misfiled and
  will conflict with the research that borrows it next.

## Sharing

**A share link never exposes a skill** — not the index, not a body. Which
methodology a team follows is working process, the same class as memory, and none of it is on the public surface. There are no skills routes
under `/api/shared/{token}/`, and `skill_load` refuses a share context on its own
so a route added later still fails closed.

## REST

Slugs address a skill only inside a research, where the resolution order is
defined. Management is by id, because a built-in and a fork of it share a slug.
Thirteen routes, all of them under the ordinary authenticated API:

| Method | Path | Does |
|---|---|---|
| `GET` | `/api/researches/{id}/skills` | what it follows, in precedence order, always including the product skills. `{data, cap, chosen}` — `cap` is the server's number so no client carries its own, `chosen` counts only what somebody picked, so it is legitimately smaller than the list |
| `GET` | `/api/researches/{id}/skills/library?q=` | what it may attach: the non-ambient built-ins and its team's library, each with `attached`. The always-on product skills are absent — they are already on, so offering them is a control with nothing behind it. `q` matches name or description. Research-private skills are never listed, its own included |
| `GET` | `/api/researches/{id}/skills/{slug}` | one skill **with its body** — the same resolution `skill_load` uses |
| `POST` | `/api/researches/{id}/skills` | `{slug}` attaches an existing skill; `{name, description, body}` writes a research-private one and attaches it. `201` |
| `DELETE` | `/api/researches/{id}/skills/{slug}` | detach. `204`. Ambient is refused |
| `PUT` | `/api/researches/{id}/skills/{slug}` | **built-in only** — copies it into the team with your edit and moves the attachment to the copy in one step. Answers `{data, forked: true}`; follow the slug to the new row. A team or private skill here is `not found`, not an update |
| `POST` | `/api/researches/{id}/skills/{slug}/copy` | team or built-in → a research-private copy, attachment swapped. Already private: returns it unchanged |
| `POST` | `/api/researches/{id}/skills/{slug}/promote` | research-private → the team library, attachment follows, **the private original is deleted**. Anything else is `not_allowed` |
| `GET` | `/api/teams/{id}/skills` | a team's library on its own terms, for a team with no research yet |
| `GET` | `/api/skills/{skillId}` | one skill with its body, addressed the way `PUT` and `DELETE` are |
| `POST` | `/api/teams/{id}/skills` | write a team skill. Created unattached — attach it to a research separately |
| `PUT` | `/api/skills/{skillId}` | edit a team or private skill in place. A built-in is `not_allowed`: editing one is the fork route above |
| `DELETE` | `/api/skills/{skillId}` | delete a team or private skill. A built-in is `not_allowed`; a team skill other researches follow is `skill_in_use` |

Every write needs `editor` or better, asked of whoever owns the thing being
changed: the research for the `/api/researches/{id}/skills…` routes, the team for
`/api/teams/{id}/skills` and for forking or promoting into a library, and — for
the by-id routes — whichever of the two the skill belongs to. A `viewer` gets
`403`, a non-member `404`. With `auth_enabled: false` there is nobody to check
and every route is permitted — to a caller the server accepts a write from at
all: with no `api_token` configured either, that is one on the server's own
machine and nobody else.

Conflicts carry a `code` so a client can tell them apart without matching on
prose:

| `code` | Status | Means |
|---|---|---|
| `skill_cap_reached` | 409 | six chosen skills already, on attach or on writing a private one |
| `already_attached` | 409 | this research follows that skill already |
| `slug_taken` | 409 | a skill of that name exists in the same tier scope |
| `skill_in_use` | 409 | a team skill other researches follow; the message carries the count |
| `not_allowed` | 403 | detaching an ambient skill, writing to a built-in, promoting something that is not private |

Validation failures are `400` and carry a `field`: `name` (empty),
`description` (over 200), `body` (empty or over 16000).

The MCP tools answer with the same codes, prefixed to the message rather than
carried in a status — there is no status to carry them in.
