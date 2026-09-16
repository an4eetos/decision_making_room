# Conversation modes

A mode is what kind of question you are asking. It decides three things: the
instructions the model gets, the shape the answer takes, and what retrieval
favours.

Fifteen ship. The most important is `open`, which imposes no template at all —
without it, every casual question gets forced into a framework, which is worse
than having no modes.

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

## Writing your own

Set `MODES_DIR` to a directory of markdown files. A file whose `id` matches a
shipped mode replaces it; a new `id` is appended.

```markdown
---
id: my_mode
name: My Mode
family: plan            # plan | decide | unblock | review | open
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
