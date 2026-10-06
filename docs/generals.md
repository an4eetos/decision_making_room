# Generals and working styles

Two rosters. **Generals** are strategic lenses — you pick up to three and they
argue about what to do. **Working styles** describe how you execute once the
decision is made; modes reference them, you do not pick them by hand.

## How a general reaches an answer

Each general is a markdown file with YAML frontmatter. The frontmatter becomes
a *card* — name, job, when it fits, when it is wrong, how it sounds, and its
blind spot — and the card goes into every answer the general is on. The
markdown body below it is the full doctrine, and it never goes in whole.

An earlier version of this tool concatenated every general's complete text into
every prompt: roughly 5,000 tokens of a 12,000-token always-on context block, on
every question, whether or not any of it was relevant. Cards alone fixed the
cost but made the doctrine decorative — the model argued from what it
remembered about the historical figure instead.

So the doctrine is split into **passages**: one per `##` section, with long
sections split at paragraph breaks into pieces of at most 900 characters
(about 225 tokens). For each general on an answer, the passages closest to the
question go under its card:

| Tier | Generals | Passages per general |
|---|---|---|
| quick | 1 | 1 |
| standard | 2 | 1 |
| deep | 3 | 2, plus the `read_doctrine` tool for the rest |

Passages are ranked by embedding similarity to the question. The question
vector is the one retrieval already computed, and passage vectors are embedded
once and cached in Postgres (`doctrine_embeddings`), so choosing costs no model
call and no embedding call. Until the cache is built on first start, or if the
embedder is down, ranking falls back to word overlap. A mode can prefer
sections through `generals.doctrine` in its frontmatter — a pre-mortem leans on
*The case against* and *Where it broke* — which breaks near-ties without
overriding the question.

Doctrine length is therefore free: a general can have ten sections, and only
the relevant one is paid for. What costs is paragraph length, because a
paragraph never splits; a test fails on any paragraph over 900 characters.

## The exchange

When two or three generals answer together, they do not give parallel
opinions. Each states an opening position, then **answers the strongest point
another made** — not a weaker version of it — and **concedes** the one thing the
other side has right, without a "but". The answer then names **the fork**, the
real disagreement as a question you have to answer, and **the call**: who it
sides with, what it takes from the others, and what siding costs.

It is still one model call. What makes it work is that each general carries
four extra fields into the prompt:

- `asks` — the questions it always puts to a situation, which the others can
  turn against it.
- `concedes_when` — the conditions under which it yields. A lens that can never
  be wrong cannot take part in a real exchange.
- `unknowns` — one line on how it sorts what is not known and what it does
  about it. Lenses disagree about the unknown at least as much as about the
  known: Patton wants to touch it today, Kutuzov wants time to answer it,
  Chuikov ignores every unknown that does not threaten the minimum.
- `rivals` — the generals it most naturally argues with. When there is room for
  more than one lens, a rival of the leading lens is seated beside it, so the
  answer has an actual fork in it.

## Selection

If you pick generals in the UI, those are used. Otherwise selection is
deterministic — keyword routing over the question, a nudge toward families that
suit it, and a penalty for lenses used in the last two turns so one lens does not
answer everything.

Within a conversation the roster is sticky. Once generals have answered, they
keep answering the follow-ups, shown as **auto · kept**. Like a mode, the roster
only changes when the situation does:

- **The mode switches.** The bar to leave a mode is already high, so clearing it
  earns a fresh pick of generals as well.
- **A general off the bench makes a strong case.** It needs two distinct routing
  phrases in the question and must outscore the weakest sitting general by a
  full keyword hit. It then takes that one seat, not the whole roster.
- **The depth goes up.** The extra seats are filled around the sitting roster.

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

Each file's body — sent to the model a passage at a time, as above — has eleven
standard sections, and a test fails if any is missing:

- **Doctrine**: the core claim, as statements. What the lens believes and does.
- **Cognitive strengths**: what this lens sees that most minds miss. Zhukov
  goes to the worst number first; Kutuzov separates the discomfort of waiting
  from its cost.
- **Traps it kills**: how it takes apart each trap in its `kills` list, in its
  own register. See [Traps](#traps) below.
- **Under interrogation**: what it pushes on when questioning, which hidden
  evaluations it refuses to let stand, and its questions. This is what an
  interrogation argues from.
- **In the pocket**: how it behaves when pressure closes from several sides at
  once. This is where the lenses differ most. Zhukov breaks out at the thinnest
  point, Kutuzov gives up the city to save the army, Slim forms a box and
  holds, Giáp dissolves, Yi Sun-sin retreats to the narrow water. Written as
  **numbered orders**, each followed by *On a problem:* — what the order means
  on an ordinary one.
- **With reserves in hand**: how it spends abundance: runway, savings, a free
  season. Guderian never disperses, Kutuzov does not spend, Boyd buys learning
  speed, Manstein drops the cleverness and takes the straight road. Also
  numbered orders with *On a problem:* lines; a test checks both sections.
- **The case against**: the strongest critique, written seriously.
- **Where it broke**: one short paragraph on a real failure (someone else's,
  labelled as such, when the general's own record has none).
- **Rivals**: how it argues with its opponents, and where they meet.
- **Over a long game**: which phase of a long effort it owns, and who takes over.
- **Facing the unknown**: how it sorts uncertainty, what it asks, and how it
  handles fear and an imperfect present.

Between Doctrine and In the pocket, each general carries two or three
**signature sections** named for the situation they address: Zhukov's *Bad news
first*, Eisenhower's *Own the decision before you make it*, Rokossovsky's *Hot
and cold*. Headings are embedded with the text, so name them for the situation
a question would describe.

Write statements, not history. A section says what the lens does and why it
works, in the lens's own register: short, bold, and specific. History appears
only where it proves a mechanism, in a clause, and mostly in Where it broke. Where
decision research backs or limits a claim (action bias, loss aversion,
checklists, threat-rigidity), use the finding plainly without turning the
section into a literature review. See `zhukov.md` for the full form.

A lens is a phase, not a policy. Over a long game exists because most real
questions are not one decision but a sequence of them, and the lens that is
right in month one is usually wrong by month six.

A general's rivals must come from different families from each other and from
the general itself. Selection seats all of them beside the leading lens, so two
rivals from one family would crowd out the third perspective the exchange
needs; a test enforces this for the shipped roster.

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
asks:
  - A question it always asks
unknowns: One line. How it sorts uncertainty and what it does about it.
concedes_when:
  - A condition under which it yields
rivals: [kutuzov]          # ids of generals it argues with, distinct families
kills: [encircled, impact_bias]   # 2-4 trap ids from traps.yaml
orders:                    # exactly three, one line each, usable on a real problem
  - Hit the whole front at once, so no sector can be reinforced from another.
  - Prepare in secret, everywhere, so the main blow cannot be read in advance.
  - Judge by what moved this week, not by the plan.
routes:
  keywords: [words, "or phrases", that, route, here]
portrait:                  # optional; file goes in internal/web/assets/static/portraits/
  file: brusilov.jpg
  credit: Photographer or painter
  license: Public domain
  source: https://commons.wikimedia.org/wiki/File:...
---

Everything below the frontmatter is doctrine: split into passages at each ##
heading, and sent a passage at a time when it bears on the question.
```

The roster is validated at startup and a malformed file aborts the boot. That is
deliberate — a lens missing its job or its blind spot still renders a card, so
nothing further down the stack could tell it had been silently degraded.

## Traps

`internal/generals/assets/traps.yaml` is the catalogue of cognitive traps the
room watches for: the spotlight effect (public shame forecast as a verdict), a
pain forecast too large, imagined misunderstanding, encirclement passivity
("no choice"), sunk cost, normalcy bias, vague intent and the rest. Each has a
`tell`, the `signals` that suggest it, and the `kill` question that exposes it.

Every general names the traps it kills, and every trap must be killed by at
least one general; startup fails otherwise. The kills decide two things: which
seated general signs a suggestion to be interrogated, and which traps an
interrogation hunts. They stay out of the card, which is at its budget.

Traps are found two ways, neither costing an extra model call. Signal phrases
match the question deterministically; a trap marked `weak` (common in ordinary
speech, like "try to") only counts beside one that is not. And the answering
model may end its answer with a `[[interrogate: id, id]]` line for traps it saw
between the lines; the line is cut out before the answer is stored or shown.

Override or extend the catalogue with `GENERALS_DIR/traps.yaml`: a matching id
replaces the entry, a new id is appended.

## Portraits

Nineteen of the twenty have portraits from Wikimedia Commons, stored at 400
pixels wide in `internal/web/assets/static/portraits/`. Each general's file
records the author, licence and source, and
[docs/portrait-credits.md](portrait-credits.md) lists them together. Three are
CC BY-SA and carry the attribution line the licence asks for. Boyd has no freely
licensed portrait and shows a monogram, as does any general whose image fails to
load.
