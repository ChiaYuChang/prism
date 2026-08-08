# Prism Semantic Analysis Pipeline

This document defines the semantic analysis stages used by Prism.

The pipeline is intentionally stage-oriented: each stage has a clear semantic responsibility, consumes durable outputs from prior stages, and produces durable artifacts for downstream stages. Stage boundaries are logical barriers; work inside a stage may still fan out into parallel tasks where appropriate.

## Pipeline Overview

```text
Stage 0 — Prepare
Stage 1 — Article Extraction
Stage 2 — Canonicalization
Stage 3 — Framework Construction
Stage 4 — Article × Topic Evaluation
Stage 5 — Coverage
Stage 6 — Final Corpus Analysis
```

## Cross-Stage Principles

- Stage-level execution is sequential, while tasks within a stage may execute in parallel.
- Every stage operates on the immutable analysis input snapshot associated with the current analysis execution.
- Semantic artifacts are durable and versioned. Tasks describe work; artifact tables describe semantic results.
- Earlier-stage outputs are not mutated by later stages. Later stages create new artifacts that reference earlier ones.
- Raw article wording must be preserved. Canonicalization adds mappings; it does not rewrite the original extraction.
- JSON property names, enum values, identifiers, and schema-defined literals remain fixed in English.
- Natural-language content produced by LLM stages is written in Traditional Chinese (`zh-TW`).
- LLM stages must not invent facts that are absent from their inputs.
- Stage 1 is article-local and should remain reusable across different article groups whenever the same immutable article input and model/prompt/schema versions are used.
- Stage 2 and later stages are corpus-dependent and belong to the current analysis execution or an explicitly versioned corpus-level artifact.

---

# Stage 0 — Prepare

## Goal

Create a complete, immutable, reproducible execution context for the semantic pipeline.

## Description

Stage 0 converts the user's selected article set into the stable input boundary used by all later stages.

It is responsible for resolving the selected candidates into usable article content, applying the configured fetch-failure policy, creating or attaching the `AnalysisExecution`, and materializing the immutable input snapshot.

This stage performs no semantic interpretation of the news corpus. Its purpose is to guarantee that every later stage sees the same article set and the same article content even if the live database records change after execution begins.

Stage 0 also establishes the operational pipeline plan and the durable identifiers required to trace all later semantic artifacts back to the execution that produced them.

## Input

- `AnalysisRun`
- user-selected candidate IDs
- topic and optional analysis brief
- fetch-failure policy / user resolution decisions
- current candidate/content records required to resolve the selection
- deployed pipeline definition and version/hash

## Output

- `AnalysisExecution`
- immutable selected candidate snapshot
- immutable selected content/article snapshot
- stable list of article/content IDs participating in the execution
- root pipeline batch / execution control records
- execution fingerprint and pipeline-definition provenance

Conceptually:

```text
PreparedExecution
  execution_id
  analysis_run_id
  selected_articles[]
    candidate_id
    content_id
    title
    publisher
    published_at
    article_text
    source metadata
  topic
  brief
  pipeline_definition_hash
  input_snapshot_time
```

---

# Stage 1 — Article Extraction

## Goal

Convert each individual article into a faithful, structured, article-local semantic representation.

## Description

Stage 1 parses every selected article independently.

The stage should answer questions such as:

- What is the article mainly saying?
- Which propositions are factual claims, opinions, or ambiguous/mixed statements?
- Who is responsible for each attributed statement?
- Is a statement directly quoted, indirectly attributed, or unattributed?
- Which people, organizations, places, facilities, policies, events, or other important entities are mentioned?
- Which propositions are primary to the article, which are supporting, and which are only mentioned?
- What evidence in the article supports each extracted proposition?

Stage 1 must distinguish between a claim being reported and the claim being objectively true. For example, if a politician says that a policy will raise electricity prices, Stage 1 records that the politician made the claim; it does not independently verify the prediction.

Stage 1 must preserve the article's original surface terminology. It must not normalize `核三`, `核三廠`, and `第三核能發電廠` into one term. That is the responsibility of Stage 2.

Stage 1 should not use corpus-level controversy topics, other articles, expected political positions, or later-stage framework information.

This stage naturally fans out: one article extraction task may be run independently for each selected article.

## Input

For one article:

- immutable article/content snapshot from Stage 0
  - title
  - publisher
  - published time, when available
  - article body/text
  - stable content identity/provenance
- Stage 1 model version
- Stage 1 prompt version
- Stage 1 output-schema version

Must not include:

- other articles in the selected corpus
- canonical vocabulary from Stage 2
- Stage 3 framework topics
- expected stance or party alignment

## Output

One durable extraction artifact per article.

Conceptually:

```text
ArticleExtraction
  article/content identity
  summary
  propositions[]
    statement
    kind
      factual
      opinion
      mixed_or_uncertain
    attribution
      source
      source type
      named / unnamed
      direct / indirect / unattributed
    importance
      primary
      supporting
      mentioned
    evidence
  entities[]
    surface form
    entity type
    evidence
  article-level signals
  raw structured model output
  model / prompt / schema provenance
```

The exact JSON contract is defined separately by the Stage 1 prompt and schema specification.

---

# Stage 2 — Canonicalization

## Goal

Identify different surface forms across the corpus that safely refer to the same real-world entity or equivalent terminology, and assign a stable canonical representation.

## Description

Stage 2 is the first corpus-level semantic stage.

It runs only after all Stage 1 article extractions are available so that it can inspect terminology across the complete selected corpus.

Its primary responsibility is vocabulary deduplication and resolution, not controversy interpretation.

Examples:

```json
{
  "canonical": "第三核能發電廠",
  "aliases": ["核三", "核三廠"]
}
```

```json
{
  "canonical": "賴清德",
  "aliases": ["賴總統", "總統賴清德"]
}
```

Only surface forms that can safely be treated as the same entity or equivalent term should be placed in one alias group.

Semantically related but non-equivalent concepts must not be collapsed. For example, `核三延役` and `核三重啟` may be closely related in a political debate, but they should not automatically become aliases if they can represent materially different claims or policies.

Canonicalization is additive: the Stage 1 extraction remains unchanged, and Stage 2 creates mappings from original surface forms to canonical forms.

## Input

- all completed Stage 1 article extractions for the current analysis execution
- extracted entity surface forms
- extracted important terminology or proposition terms required for resolution
- optional article-local evidence needed to disambiguate ambiguous aliases
- Stage 2 model/prompt/schema versions, if implemented with an LLM

Stage 2 should normally consume Stage 1 artifacts rather than reread full article text unless additional context is necessary for safe disambiguation.

## Output

A corpus-level canonicalization artifact.

Minimal conceptual form:

```text
CanonicalizationResult
  groups[]
    canonical
    aliases[]
```

Potential extended form when required:

```text
CanonicalizationResult
  groups[]
    canonical
    aliases[]
    type
    source references / evidence
```

Important invariant:

```text
alias -> canonical
```

must represent a safe equivalence mapping, not merely topical similarity.

---

# Stage 3 — Framework Construction

## Goal

Construct a corpus-level analytical framework that defines the major controversy topics and the rubric used to evaluate how individual articles discuss them.

## Description

Stage 3 interprets the selected article group as a whole.

Using the Stage 1 extractions and Stage 2 canonical vocabulary, it identifies the principal controversy points shared across the corpus and defines each topic precisely enough for consistent downstream evaluation.

The framework is not yet the final conclusion about which topics are truly Major or Minor. Stage 3 may assign provisional classifications, but those classifications can later change after Stage 4 evaluations and Stage 5 deterministic coverage are available.

Each framework topic should include:

- a stable topic identity
- a concise topic name
- a clear definition
- inclusion/exclusion boundaries
- a 1–5 evaluation rubric
- provisional `MAJOR` / `MINOR` classification when applicable
- any additional definitions required by Stage 4

The framework should avoid duplicate topics caused only by wording differences already resolved in Stage 2.

A framework revision is immutable. Explicit framework regeneration or manual revision should create a new revision rather than modifying an existing one.

## Input

- all Stage 1 article extraction artifacts for the selected corpus
- Stage 2 canonicalization result
- analysis topic and optional brief from Stage 0
- Stage 3 model version
- Stage 3 prompt version
- Stage 3 schema version

## Output

A versioned framework revision containing corpus-level topics and scoring rubrics.

Conceptually:

```text
FrameworkRevision
  framework_revision_id
  analysis_execution_id
  topics[]
    topic_id
    name
    description
    inclusion_boundary
    exclusion_boundary
    provisional_type
      MAJOR
      MINOR
    rubric
      score_1_definition
      score_2_definition
      score_3_definition
      score_4_definition
      score_5_definition
  model / prompt / schema provenance
```

The framework revision ID becomes an explicit dependency of Stage 4.

---

# Stage 4 — Article × Topic Evaluation

## Goal

Apply one exact Stage 3 framework revision consistently to every selected article and determine how that article treats each framework topic.

## Description

Stage 4 evaluates the Cartesian relationship between articles and framework topics.

For every selected article and every Stage 3 framework topic, the stage determines whether the article actually discusses that topic and, when it does, evaluates the article according to the topic's rubric.

The critical semantic distinction is:

```text
ABSENT != low score
```

An article that does not discuss a topic must not receive score `1` merely because the topic is absent.

The stage therefore uses an explicit mention status such as:

```text
PRESENT
ABSENT
UNCERTAIN
```

Only `PRESENT` evaluations receive a normal 1–5 rubric score. `ABSENT` and normally `UNCERTAIN` evaluations carry no rubric score.

Where useful, the evaluation also records stance, reasoning, evidence, and model confidence. Confidence is an internal analysis field and should not be presented as a definitive public truth score.

This stage can fan out by article. Each article-level task can evaluate all framework topics for that article, or finer-grained fan-out may be used later if necessary.

## Input

For each selected article:

- exact Stage 3 framework revision
- Stage 1 extraction for the article
- Stage 2 canonicalization mappings relevant to the article
- article/content identity and immutable provenance
- Stage 4 model version
- Stage 4 prompt version
- Stage 4 schema version

## Output

One durable evaluation per article × framework topic.

Conceptually:

```text
ArticleTopicEvaluation
  framework_revision_id
  topic_id
  article/content_id
  mention_status
    PRESENT
    ABSENT
    UNCERTAIN
  score              # 1..5 only when PRESENT
  stance
  reason
  evidence
  confidence
  model / prompt / schema provenance
```

Required invariant:

```text
PRESENT   -> score is present
ABSENT    -> score is null
UNCERTAIN -> score is normally null
```

---

# Stage 5 — Coverage

## Goal

Compute deterministic corpus-level coverage statistics from Stage 4 evaluations.

## Description

Stage 5 is a deterministic analytical stage rather than an LLM interpretation stage.

For each framework topic, it measures how widely the topic is actually represented across the selected article corpus using the explicit Stage 4 mention statuses.

Coverage must be derived from persisted Stage 4 results rather than inferred again from article text.

The exact formula must be versioned so that historical analyses remain reproducible if the formula changes in the future.

At minimum, Stage 5 should count:

- number of `PRESENT` evaluations
- number of `ABSENT` evaluations
- number of `UNCERTAIN` evaluations
- eligible article count
- raw coverage ratio

Additional coverage variants may be added later, but the raw deterministic calculation should remain independently available.

## Input

- exact Stage 3 framework revision
- complete Stage 4 article × topic evaluation set
- selected corpus membership from Stage 0
- coverage formula version

No LLM call is required.

## Output

A versioned coverage snapshot.

Conceptually:

```text
CoverageSnapshot
  framework_revision_id
  formula_version
  topics[]
    topic_id
    present_count
    absent_count
    uncertain_count
    eligible_count
    raw_coverage
  input_hash
```

The snapshot is immutable and becomes an explicit input to Stage 6.

---

# Stage 6 — Final Corpus Analysis

## Goal

Produce the final corpus-level semantic conclusions using the framework, actual article evaluations, and deterministic coverage results.

## Description

Stage 6 revisits the provisional Stage 3 framework after observing how the framework actually behaves across all selected articles.

It is responsible for final semantic decisions such as:

- whether each controversy topic should finally be classified as `MAJOR` or `MINOR`
- whether and why that classification changed from Stage 3
- what broad consensus exists across the corpus
- where important disagreements or divergent framings remain
- a corpus-level summary suitable for later deterministic report materialization

Stage 6 must not overwrite the Stage 3 framework. Instead, it produces final decisions that reference the exact framework revision and exact Stage 5 coverage snapshot.

This preserves the analytical history:

```text
Stage 3:
  Topic X -> provisional MAJOR

Stage 5:
  raw coverage -> 52%

Stage 6:
  Topic X -> final MINOR
  changed -> true
  reason -> ...
```

Stage 6 may use an LLM for final synthesis, but deterministic numeric quantities such as coverage must come from Stage 5 rather than being recalculated or guessed by the model.

## Input

- Stage 3 framework revision
- complete Stage 4 article × topic evaluations
- Stage 5 coverage snapshot
- Stage 2 canonicalization result where terminology resolution is useful for synthesis
- Stage 1 article extractions where supporting semantic detail is required
- analysis topic and optional brief
- Stage 6 model version
- Stage 6 prompt version
- Stage 6 schema version

## Output

A durable final corpus-analysis artifact.

Conceptually:

```text
FinalCorpusAnalysis
  framework_revision_id
  coverage_snapshot_id
  topic_decisions[]
    topic_id
    final_type
      MAJOR
      MINOR
    changed_from_provisional
    reason
    consensus_score        # optional, if retained by the final design
    consensus_summary
    disagreement_summary
  corpus_summary
  important_findings[]
  model / prompt / schema provenance
```

This output is the final semantic artifact of the LLM analysis pipeline. Downstream statistical analysis and report rendering should consume persisted semantic results without rerunning Stages 1–6.

---

# Dependency Summary

```text
Stage 0 — Prepare
    |
    v
Stage 1 — Article Extraction
    |
    v
Stage 2 — Canonicalization
    |
    v
Stage 3 — Framework Construction
    |
    v
Stage 4 — Article × Topic Evaluation
    |
    v
Stage 5 — Coverage
    |
    v
Stage 6 — Final Corpus Analysis
```

### Dependency characteristics

| Stage | Scope | Typical execution shape | LLM required? |
|---|---|---|---|
| Stage 0 | Execution | single orchestration flow | No |
| Stage 1 | Per article | fan-out | Yes |
| Stage 2 | Corpus | single barrier task | Likely yes |
| Stage 3 | Corpus | single barrier task | Yes |
| Stage 4 | Per article × topics | fan-out | Yes |
| Stage 5 | Corpus | deterministic task | No |
| Stage 6 | Corpus | single barrier task | Yes |

# Out of Scope for Stages 0–6

The following happen after the semantic pipeline and should not be folded into the stage definitions above:

- PCA / MDS
- publisher-level aggregation
- chart preparation
- word-cloud generation
- R-based statistical analysis
- final report-data materialization
- HTML / Markdown rendering
- report retrieval and serving

These consumers should use already-persisted Stage 0–6 artifacts and must not implicitly rerun semantic analysis.
