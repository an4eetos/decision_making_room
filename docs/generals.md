# Generals and working styles

Two rosters. **Generals** are strategic lenses — you pick up to three and they
argue about what to do. **Working styles** describe how you execute once the
decision is made; modes reference them, you do not pick them by hand.

## How a general reaches an answer

Each general is a markdown file with YAML frontmatter. Only the frontmatter
becomes a *card* — name, job, when it fits, when it is wrong, how it sounds, and
its blind spot. The markdown body below it is full doctrine that is stored and
never injected.

That split is the point. An earlier version of this tool concatenated every
general's complete text into every prompt: roughly 5,000 tokens of a 12,000-token
always-on context block, on every question, whether or not any of it was
relevant. A single card costs about 260 tokens and three cost about 730 including
the instructions that structure the answer.

## Selection

If you pick generals in the UI, those are used. Otherwise selection is
deterministic — keyword routing over the question, a nudge toward families that
suit it, and a penalty for lenses used in the last two turns so one lens does not
answer everything.

There is no model call in this path. An LLM router would add latency to every
turn to make a choice that keyword routing usually gets right, that you can
override in one click, and that the interface shows you either way.

How many lenses you get follows the answer depth: one for quick, two for
standard, three for deep. When fewer lenses clear the relevance bar than the
depth allows, the remaining slots are filled from *opposing* families — the table
of complements in `internal/generals/service/select.go`. Two scouting lenses
agree with each other and produce nothing a single lens would not have said.

## The blind spot field is required

Every lens must declare what it systematically gets wrong, and the loader refuses
to start without it. This is not documentation. A multi-lens answer where every
lens is right about everything collapses into three voices agreeing, which is
worth nothing over one voice. The blind spots are what make the disagreement
real, and the prompt is explicit that disagreement must not be softened into
consensus.

## On the Wehrmacht generals

Manstein, Rommel and Guderian served the Third Reich. Manstein was convicted of
war crimes. Their files here describe operational thinking — concentration of
force, reading terrain, committing to a single point — and nothing else.

Including them is a judgement that the operational ideas are worth studying
separately from the regime they served, which is also why they are taught in
military academies in countries that fought against them. It is not an
endorsement of the men or of what they served, and the files do not treat them as
admirable. If you would rather not have them, delete the three files, or override
them through `GENERALS_DIR`.

## Adding or changing a general

Set `GENERALS_DIR` to a directory containing `generals/` and `styles/`
subdirectories. A file whose `id` matches a shipped one replaces it; a new `id`
is appended. Nothing needs recompiling and the repository does not need forking.

```yaml
---
id: brusilov
name: Brusilov
epithet: The Broad Front
era: Russian Empire, First World War
family: contact          # contact | scouting | endurance | adaptation
                         # systems | concentration | preservation
job: One sentence. What this lens is for.
deploy_when:
  - when it fits
avoid_when:
  - when it is the wrong tool
sounds_like: How an order in this voice actually sounds.
bias: What it systematically gets wrong. Required.
routes:
  keywords: [words, "or phrases", that, route, here]
---

Everything below the frontmatter is doctrine: stored, searchable, and never put
into a prompt by default.
```

The roster is validated at startup and a malformed file aborts the boot. That is
deliberate — a lens missing its job or its blind spot still renders a card, so
nothing further down the stack could tell it had been silently degraded.
