## FINAL RANKED ENHANCEMENT PLAN
## Supermemory → MindBank Enhancements
## Method: Darwin 8-dimension scoring + Praxis gap-analysis
## Date: 2026-05-09
## Confidence: HIGH (85%)

---

## EXECUTIVE SUMMARY

Supermemory has 6 core innovations. After Darwin scoring (8-dimension rubric) and Praxis gap-analysis (7 checks + system reality verification), we have:

| Rank | Enhancement | Darwin Score | Praxis Verdict | Priority |
|------|-------------|--------------|----------------|----------|
| 1 | **Automatic Forgetting** | **86.0** | STRONG ADOPT | **P1** |
| 2 | **User Profiles** | **76.8** | ADOPT (modified) | **P1** |
| 3 | **Enhanced Hybrid Search** | **74.5** | ADOPT (phased) | **P2** |
| 4 | **Contradiction Detection** | **67.3** | ADOPT (manual first) | **P2** |
| 5 | Connectors | 56.8 | DEFER | P3 |
| 6 | Multi-modal Extractors | 48.5 | REJECT | - |

**Decision: Hybrid adoption over replacement.** MindBank's graph structure is more flexible than Supermemory's ontology. Local/self-hosted is preferred.

---

## P1: AUTOMATIC FORGETTING (Score: 86.0)

### What it is
Temporal expiry system: nodes get a TTL based on type. Old nodes marked "superseded" but preserved. Search filters them out by default.

### Why it scores highest
- Leverages existing temporal versioning (ValidTo field)
- Natural extension of current architecture
- Solves real problem: 6,151 nodes, 77% are sessions → database bloat
- Fully reversible (Type 2)

### Implementation
```
Schema: Add expires_at (timestamp, nullable, default null) to nodes table
Policies: session=30d, fact=90d, preference=365d, profile=never
Cron: Daily job marks expired nodes (sets valid_to = now, not delete)
Search: WHERE valid_to IS NULL (existing filter already works!)
Pin: metadata.important = true → bypasses TTL
```

### System reality verified
- Node model has ValidTo → CONFIRMED
- Update() creates new version → CONFIRMED
- Search already filters valid_to IS NULL → CONFIRMED
- Need: new `expires_at` field + cron job + pin flag

---

## P1: USER PROFILES (Score: 76.8)

### What it is
Structured user facts extracted from nodes. Static (name, role) + dynamic (recent topics, preferences). Augments search queries.

### Why it scores high
- Builds on Auto Forgetting (profiles have longer TTL)
- Provides query context → more relevant search
- Simple schema: profile table with category, fact, confidence, source_node_id

### Implementation
```
Schema: profiles table (id, category, fact, confidence, source_node_id, valid_from, valid_to)
Extraction: On node creation, if node_type = preference/fact/decision → extract to profile
Update: Newer fact supersedes older (same category)
Search: Query "hnsw" → augmented to "hnsw (user prefers over ivfflat)"
Approval: Profile facts require confidence >0.9 OR user manual approval
```

### System reality verified
- Node types include preference, fact, decision → CONFIRMED
- Metadata field stores JSON → CONFIRMED (profile can store there too)
- Need: new profiles table + extraction logic + query augmentation

---

## P2: ENHANCED HYBRID SEARCH (Score: 74.5)

### What it is
Search across both memories AND external documents in one query. Phase 1: separate tab. Phase 2: unified ranking with RRF.

### Why phased
- Most complex of top 4
- Requires document ingestion pipeline (not built yet)
- Risk of regression in existing search

### Implementation
```
Phase 1 (now): Add "Documents" namespace + tab in dashboard
  - Text paste into document nodes (existing node creation)
  - Search scoped to namespace
Phase 2 (later): Unified search
  - Parallel search: memory_index + document_index
  - RRF merge with source labeling
  - Feature flag: unified_search_enabled
```

### System reality verified
- Namespace system exists → CONFIRMED
- Hybrid search (FTS+vector) exists → CONFIRMED
- Need: document chunking + separate index (or reuse existing)

---

## P2: CONTRADICTION DETECTION (Score: 67.3)

### What it is
Detect when two nodes conflict (e.g., "ivfflat is best" vs "hnsw is best"). Mark with EdgeContradicts or EdgeSupersededBy.

### Why manual first
- Highest false-positive risk
- Embedding similarity threshold needs tuning
- User should approve contradiction marking

### Implementation
```
Phase 1 (manual): User flags contradiction → create EdgeContradicts
Phase 2 (auto): On node creation, check embedding similarity to existing nodes
  - If similarity >0.85 AND content polarity detected → suggest contradiction
  - User approves in dashboard "Review" queue
```

### System reality verified
- EdgeContradicts edge type exists → CONFIRMED
- EdgeSupersededBy edge type exists → CONFIRMED
- EdgeRepo.Create() works → CONFIRMED
- Need: embedding comparison logic + review queue UI

---

## REJECTED

### Multi-modal Extractors (Score: 48.5)
- Too complex for current stage
- Heavy dependencies (OCR, PDF extraction)
- YAGNI: User can paste text

### Connectors (Score: 56.8)
- Cloud API dependencies conflict with local-first philosophy
- OAuth token storage is security risk
- DEFER until after core features stable

---

## IMPLEMENTATION ORDER

1. **Auto Forgetting** (1-2 days)
   - Schema migration: add expires_at
   - Cron job: mark expired
   - Pin flag: metadata.important
   - Dashboard: "Expiring Soon" section

2. **User Profiles** (2-3 days)
   - Schema: profiles table
   - Extraction: hook into node creation
   - Search augmentation: query context
   - Dashboard: profile editor

3. **Enhanced Hybrid Search** (3-5 days, phased)
   - Phase 1: Documents namespace + tab
   - Phase 2: Unified search with RRF

4. **Contradiction Detection** (2-3 days, phased)
   - Phase 1: Manual contradiction edges
   - Phase 2: Auto-suggest with review queue

---

## FILES PRODUCED

- `/home/rat/mindbank/docs/supermemory-enhancement/test-prompts.json` — Darwin test prompts
- `/home/rat/mindbank/docs/supermemory-enhancement/darwin-scoring.md` — 8-dimension scoring for all 6 candidates
- `/home/rat/mindbank/docs/supermemory-enhancement/gap-analysis.md` — Praxis 7-check validation
- `/home/rat/mindbank/docs/supermemory-enhancement/final-plan.md` — This file

---

## NEXT STEP

Get user approval on P1 features (Auto Forgetting + User Profiles), then proceed to implementation planning with `writing-plans` skill.
