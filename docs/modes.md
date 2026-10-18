# Conversation modes

A mode is what kind of question you are asking. It decides three things: the
instructions the model gets, the shape the answer takes, and what retrieval
favours.

Fifteen ship, plus five that are never detected: `interrogation`, `beast`,
the two staff modes `staff` and `red_team`, and `after_action`, which writes
a quarter's review from the campaign record. The most important is `open`, which
imposes no template at all — without it, every casual question gets forced into
a framework, which is worse than having no modes.

## Detection

A cascade, in order:

1. **Explicit.** You pinned a mode. It is used and never overridden.
2. **Sticky.** The conversation is already in a mode. It stays there unless a
   different mode makes a strong case, because changing the shape of the answer
   underneath someone mid-conversation is more disruptive than being in a
   slightly wrong mode. The bar to switch is deliberately much higher than the
   bar to enter.
3. **Keyword.** Trigger phrases, weighted, with a bonus for matching near the
   start of the question where intent usually is. A mode must both clear a score
   threshold and clearly beat the runner-up — ambiguity falls through rather than
   guessing.
4. **Default.** `open`.

The detected mode always shows in the chip above the conversation, with how it
was arrived at. That is the point: a misdetection is one click to fix instead of
a silently oddly-shaped answer.

Two trigger phrases in different modes is a validation error. Shared phrases
score equally, the ambiguity guard rejects both, and the question silently falls
back to `open` — the loader refuses to start rather than let that happen quietly.

## Modes and lenses together

A mode decides the *shape*; generals decide the *argument inside it*.

When a mode has an output template, the lenses do not add their own sections —
they argue within the mode's structure and the disagreement surfaces in whichever
section it bears on. Without that rule the two templates get concatenated and the
answer grows a second scaffold, which is exactly what happened the first time
they were wired together.

Modes name preferred generals, which seed selection without overriding clear
evidence from the question, and can cap how many lenses an answer uses.

## Retrieval bias

Each mode nudges retrieval:

- **Kinds** are boosted, never filtered. On a corpus of a few thousand rows,
  excluding a kind throws away good hits for no gain.
- **Recency weight** overrides the global default. A debrief wants recency to
  dominate (0.40); a pre-mortem wants it nearly ignored (0.05), because a
  decision from a year ago is still the answer to a question about that decision.
  Relevance absorbs the change so weights still sum to one and scores stay
  comparable between modes.

## Working styles

Generals argue about strategy; **working styles** describe how to execute once
the decision is made. Modes reference them by id and they colour the advice
rather than being argued between. You do not pick them by hand.

## Interrogation

The one mode that does not advise. The seated generals question you, one
question each plus a single probe you can check in reality within 48 hours,
until your position no longer rests on a trap: shame nobody will remember,
pain forecast as permanent, "no choice", money already spent voting on what
happens next. Vague answers are sent back for a date, a number or a name.

- **Entered only on purpose** (`explicit_only: true`): from the mode menu, or
  from a general's banner under an answer that showed a trap. Detection never
  picks it, and once unlocked it does not stick.
- **The generals who saw the trap run it** (`keep_seated: true`): unlike any
  other mode switch, entering it keeps the seated roster.
- **Each turn** shows On the record, Still dark, Caught (with your words
  quoted) and Questions. When nothing left would change the position, Still dark
  says so and *Take a position* lights up.
- **Take a position** sends a closing turn that uses the mode's `## Position`
  section instead of `## Output`: the stance, the traps killed, orders with a
  direction, a time and a fallback, and the unknowns you chose to carry. It
  also ends the interrogation. **Leave** ends it without a position.

The room suggests an interrogation when it finds a strong trap in what you
wrote (see [Traps](generals.md#traps)). The suggestion is stored with the
answer, never made in a check-in or inside an interrogation, and held back for
four answers after the last one so it does not nag.

A mode with `explicit_only: true` needs no triggers (and may not have any). Any
mode can carry a `## Position` section and a closing turn; the request flag is
`conclude: true`.

## Beast mode

The crisis gear, for when a deadline is on top of you and you are freezing,
drifting or bargaining with yourself. The generals stop being polite. The most
aggressive one seated (ranked by the mode's `shouters`: Zhukov, Patton, Chuikov,
Konev, Suvorov, Guderian, Alexander; otherwise whoever sits first) swears at
you in English with Russian mat, in the manner Zhukov's staff remembered:
the tribunal, the penal battalion, "I'll have you shot". The others stay just
as hard but do not swear.

**Language warning:** this mode is profane on purpose. It never switches on by
itself (`explicit_only: true`).

- **The button** sits in the situation strip and pulses while an open loop is
  due today or overdue. It is also in the mode menu under Crisis.
- **Orders run on a clock.** The generals write `[[order: MINUTES | action]]`
  lines, which the server rewrites as readable lines and puts on the board
  under the composer. You can edit the text, move the clock ±5 minutes, add
  your own orders, or strike one off (it stays as *abandoned*).
- **Only a report stops a clock.** Press *Report done*, or tell them in chat
  which task is finished. The model then writes `[[done: N]]`, but only for a
  task you named.
- **A clock that runs out** makes the generals erupt on their own. The page
  sends the turn and the server writes the "Time's up" message. The task is
  re-armed at half its time per miss, never under five minutes. Eruptions are
  at least two minutes apart; clocks that run out in between are shouted about
  together.
- **Open loops due today or overdue** are put in front of the generals. An
  order marked `| loop N` finishes that loop, and reporting it closes the loop.
- **Stand down** sends a closing debrief (`## Position`) and marks whatever is
  still open as abandoned. **Leave** goes back to ordinary advice and keeps the
  clocks. They are not watched, so nobody erupts at a conversation you left.

Hard limits, written into the prompt and outranking the voice: the abuse is
aimed at excuses and stalling, never at who you are. No slurs about race,
ethnicity, nationality, religion, sexuality, gender or disability, and nothing
about your body. If you mention self-harm, a medical emergency or someone in
danger, the act drops for that turn. With Gemini, beast turns loosen only the
harassment filter, and only to blocking the worst of it; hate speech stays at
Google's default. Claude has no such knob, so with Anthropic the prompt is the
only line; a turn Claude declines is re-run server-side on Anthropic's fallback
model, and if that declines too it is reported as declined. A reply a filter
cuts off is reported as blocked rather than stored half-written.

Beast turns are kept out of open-loop and campaign extraction: the orders
already have their own board.

## The staff modes

Two modes in the `staff` family read the campaign map rather than a question.
Both are `explicit_only`: the staff reads the map on request, never because a
keyword matched.

**Staff appraisal** (`staff`) is the ordinary exchange laid over a fixed
template: the situation, one point of main effort, where to yield ground, what
is overextended, which fog to lift first, and orders. The lenses argue inside
those sections, as in any structured mode, and at depth they convene as a
council. The campaign page's *Ask the staff* runs it with a question that names
the objectives in play; pinning it in a conversation runs it on what you ask.
The map reaches it through the context like any other answer. See
[docs/campaign.md](campaign.md#the-staff).

**Red team** (`red_team`) carries `red_team: true` in its frontmatter, which
replaces the exchange with one general commanding the opposition. The loader
requires `generals.max: 1` on such a mode: two enemies argue with each other
instead of with the plan. No council, no forecast, no interrogation flag. Its
output sections are fixed and the room reads them: the attacks under *Where
the plan breaks* and the questions under *Your fog, my cover* are proposed to
the map without a model call. *Leave* goes back to ordinary advice.

## Writing your own

Set `MODES_DIR` to a directory of markdown files. A file whose `id` matches a
shipped mode replaces it; a new `id` is appended.

```markdown
---
id: my_mode
name: My Mode
family: plan            # plan | decide | unblock | review | open
                        # interrogate, beast and staff are explicit_only
summary: One line, shown in the mode menu.
triggers:
  keywords: ["a phrase", "another phrase"]
  weight: 1.0           # damp below 1 if the triggers are unavoidably generic
retrieval:
  kinds: [plan, daily_log]
  kind_boost: 0.25
  top_k: 8
  recency_weight: 0.30  # 0 uses the global default
generals:
  default: [zhukov]
  max: 2
styles: [block_operator]
# explicit_only: true   # never detected; needs no triggers
# red_team: true        # one general commands the opposition; needs max: 1
---

## System

Instructions for the model. Required.

## Output

The structure the answer should take. Optional — omit it and the answer is
unstructured, like the open mode.
```

Validated at startup; a malformed mode aborts the boot. A mode missing its
system prompt still resolves and still answers, just without the behaviour it
exists for, and nothing downstream could detect that.
