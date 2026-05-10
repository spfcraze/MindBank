## Supermemory → MindBank Enhancement Scoring
## Method: Darwin 8-dimension rubric + Praxis gap-analysis
## Date: 2026-05-09
## Scorer: Praxis + Darwin hybrid

---

## Candidate 1: USER PROFILES (Static Facts + Dynamic Context)

### Darwin Scoring

| Dimension | Score (1-10) | Weight | Weighted | Notes |
|-----------|-------------|--------|----------|-------|
| Frontmatter/Definition | 9 | 8 | 72 | Clear scope: static facts (name, role, preferences) + dynamic context (recent topics, active projects) |
| Workflow Clarity | 8 | 15 | 120 | Well-defined: extract from nodes → store in profile table → update on new facts → query augmentation |
| Edge Case Coverage | 7 | 10 | 70 | Missing: conflict resolution when user contradicts themselves, profile versioning |
| Checkpoint Design | 8 | 7 | 56 | User approval needed for profile creation, but auto-update could surprise user |
| Instruction Specificity | 8 | 15 | 120 | Concrete: profile table schema, extraction rules, update triggers |
| Resource Integration | 7 | 5 | 35 | Needs new table + API endpoints; integrates with existing search |
| Overall Architecture | 8 | 15 | 120 | Clean separation: facts vs memories, profile as query context layer |
| Live Test Performance | 7 | 25 | 175 | Test prompt 3: search augmentation works; test prompt 4: profile survives forgetting |
| **TOTAL** | | **100** | **768** | **= 76.8 / 100** |

### Praxis Gap-Analysis
- INVERSION: What if user has NO profile? → graceful fallback to unaugmented search
- SECOND-ORDER: Profile bloat over time → need compaction rules (keep top N facts per category)
- MECE: Static vs dynamic split is clean; missing: temporal profile (how preferences evolved)
- MAP/TERRITORY: Profile is a MODEL of user, not the user themselves → refresh periodically
- ADVERSARIAL: Malicious input could poison profile → sanitize extractions
- SIMPLICITY: Could start with just 3 fields: name, role, top_topics
- REVERSIBILITY: Profile can be deleted without affecting raw memories → yes

### Verdict: ADOPT with modifications

---

## Candidate 2: AUTOMATIC FORGETTING (Temporal Expiry)

### Darwin Scoring

| Dimension | Score (1-10) | Weight | Weighted | Notes |
|-----------|-------------|--------|----------|-------|
| Frontmatter/Definition | 9 | 8 | 72 | Clear: TTL per node type, configurable policies |
| Workflow Clarity | 9 | 15 | 135 | TTL set at creation → background job marks expired → search filters superseded |
| Edge Case Coverage | 8 | 10 | 80 | Missing: what if user wants to "remember forever"? → pin flag needed |
| Checkpoint Design | 9 | 7 | 63 | User configures policy upfront; no surprises |
| Instruction Specificity | 9 | 15 | 135 | Concrete: default TTLs (session=30d, fact=90d, profile=365d), cron schedule |
| Resource Integration | 8 | 5 | 40 | Uses existing temporal_versioning field; adds cron job |
| Overall Architecture | 9 | 15 | 135 | Elegant: leverages existing temporal system, adds expiry dimension |
| Live Test Performance | 8 | 25 | 200 | Test prompt 4: old ivfflat node correctly filtered; profile facts survive |
| **TOTAL** | | **100** | **860** | **= 86.0 / 100** |

### Praxis Gap-Analysis
- INVERSION: What if we NEVER forget? → database bloat, slower search, stale results
- SECOND-ORDER: Forgetting changes search behavior over time → user confusion if not visible
- MECE: Superseded vs expired vs deleted — three distinct states, well-separated
- MAP/TERRITORY: Forgetting models human memory decay → accurate, but configurable decay rates needed
- ADVERSARIAL: Accidental expiry of important info → pin/important flag + backup before mark
- SIMPLICITY: Start with 3 TTL buckets (short/medium/long) → yes
- REVERSIBILITY: Mark superseded, don't delete → fully reversible → yes

### Verdict: STRONG ADOPT — highest scoring candidate

---

## Candidate 3: CONTRADICTION DETECTION

### Darwin Scoring

| Dimension | Score (1-10) | Weight | Weighted | Notes |
|-----------|-------------|--------|----------|-------|
| Frontmatter/Definition | 8 | 8 | 64 | Detects conflicting facts, marks superseded, preserves both |
| Workflow Clarity | 7 | 15 | 105 | Complex: semantic similarity → conflict detection → user notification → resolution |
| Edge Case Coverage | 6 | 10 | 60 | Weak: false positives (related but not contradictory facts), ambiguous conflicts |
| Checkpoint Design | 7 | 7 | 49 | User should approve contradiction marking; auto-mark could be wrong |
| Instruction Specificity | 7 | 15 | 105 | Needs embedding similarity threshold, contradiction taxonomy |
| Resource Integration | 7 | 5 | 35 | New edge type + API; integrates with search ranking |
| Overall Architecture | 7 | 15 | 105 | Adds complexity to graph; benefits search quality but fragile |
| Live Test Performance | 6 | 25 | 150 | Test prompt 2: detects ivfflat vs hnsw; but false positive risk on nuanced opinions |
| **TOTAL** | | **100** | **673** | **= 67.3 / 100** |

### Praxis Gap-Analysis
- INVERSION: What if we ignore contradictions? → stale search results, user confusion
- SECOND-ORDER: Contradiction detection accuracy depends on embedding quality → threshold tuning nightmare
- MECE: Missing: partial contradiction (some aspects agree, some conflict)
- MAP/TERRITORY: "Contradiction" is model-dependent → two facts can coexist in reality (preference change)
- ADVERSARIAL: High false positive rate could annoy user → confidence score + manual review queue
- SIMPLICITY: Start with manual contradiction marking (user flags conflicts) → auto-detect later
- REVERSIBILITY: Marked contradictions can be unmarked → yes

### Verdict: ADOPT as MANUAL FIRST, auto-detect later

---

## Candidate 4: ENHANCED HYBRID SEARCH (RAG + Memory Unified)

### Darwin Scoring

| Dimension | Score (1-10) | Weight | Weighted | Notes |
|-----------|-------------|--------|----------|-------|
| Frontmatter/Definition | 8 | 8 | 64 | Unified search across documents + memories in one query |
| Workflow Clarity | 8 | 15 | 120 | Query → split into memory search + doc search → RRF merge → rank |
| Edge Case Coverage | 7 | 10 | 70 | Missing: what if doc and memory contradict? → priority rules needed |
| Checkpoint Design | 8 | 7 | 56 | Transparent: show source (memory vs doc) in results |
| Instruction Specificity | 7 | 15 | 105 | Needs RRF weights, source labeling, result formatting |
| Resource Integration | 7 | 5 | 35 | Integrates with existing FTS+vector; adds document index |
| Overall Architecture | 8 | 15 | 120 | Clean: MindBank already has hybrid search, this extends it |
| Live Test Performance | 7 | 25 | 175 | Test prompt 3: returns both sources; ranking needs tuning |
| **TOTAL** | | **100** | **745** | **= 74.5 / 100** |

### Praxis Gap-Analysis
- INVERSION: What if search stays separate? → user runs two queries, mental overhead
- SECOND-ORDER: Unified search means more results → information overload → need better ranking
- MECE: Memory vs document vs web — three sources, well-separated
- MAP/TERRITORY: "Unified" is a UI concept; backend can still run parallel searches → accurate
- ADVERSARIAL: Document results could drown memory results → weight tuning + source filters
- SIMPLICITY: Start with tabbed results (memories | documents) → unified ranking later
- REVERSIBILITY: Can disable document search → revert to memory-only → yes

### Verdict: ADOPT as phased rollout (parallel first, unified later)

---

## Candidate 5: CONNECTORS (Drive/Gmail/Notion/GitHub)

### Darwin Scoring

| Dimension | Score (1-10) | Weight | Weighted | Notes |
|-----------|-------------|--------|----------|-------|
| Frontmatter/Definition | 7 | 8 | 56 | Import external data sources into memory graph |
| Workflow Clarity | 6 | 15 | 90 | Complex: OAuth → sync → dedup → namespace mapping |
| Edge Case Coverage | 5 | 10 | 50 | Weak: rate limits, auth expiry, large file handling |
| Checkpoint Design | 6 | 7 | 42 | User approves connector setup; but auto-sync could surprise |
| Instruction Specificity | 6 | 15 | 90 | Needs connector-specific logic; not generic enough |
| Resource Integration | 5 | 5 | 25 | Heavy: new services, background workers, credential storage |
| Overall Architecture | 6 | 15 | 90 | Adds massive complexity; benefits only if user uses those tools |
| Live Test Performance | 5 | 25 | 125 | Hard to test without real accounts; mock tests insufficient |
| **TOTAL** | | **100** | **568** | **= 56.8 / 100** |

### Praxis Gap-Analysis
- INVERSION: What if we only support manual import? → less convenient but simpler
- SECOND-ORDER: Each connector is a maintenance burden → API changes break sync
- MECE: Missing: generic webhook/API connector for custom sources
- MAP/TERRITORY: Supermemory's connectors are cloud-hosted; MindBank is local → different constraints
- ADVERSARIAL: OAuth tokens stored locally → security risk → need encryption at rest
- SIMPLICITY: Start with ONE connector (GitHub, since user is developer) → expand later
- REVERSIBILITY: Imported nodes can be deleted → yes, but auth tokens persist

### Verdict: DEFER — too complex for current stage; revisit after core features stable

---

## Candidate 6: MULTI-MODAL EXTRACTORS (PDF/OCR/Video/Code)

### Darwin Scoring

| Dimension | Score (1-10) | Weight | Weighted | Notes |
|-----------|-------------|--------|----------|-------|
| Frontmatter/Definition | 6 | 8 | 48 | Extract text from non-text sources |
| Workflow Clarity | 5 | 15 | 75 | Complex: file upload → format detection → extraction → storage |
| Edge Case Coverage | 5 | 10 | 50 | Weak: corrupted files, unsupported formats, large files |
| Checkpoint Design | 6 | 7 | 42 | User uploads file; extraction happens async |
| Instruction Specificity | 5 | 15 | 75 | Needs multiple extraction libraries (tesseract, pdfminer, etc.) |
| Resource Integration | 4 | 5 | 20 | Heavy dependencies; some require external services |
| Overall Architecture | 5 | 15 | 75 | Fragile: extraction quality varies wildly by source |
| Live Test Performance | 4 | 25 | 100 | Hard to validate extraction accuracy; many edge cases |
| **TOTAL** | | **100** | **485** | **= 48.5 / 100** |

### Praxis Gap-Analysis
- INVERSION: What if we only support text paste? → less convenient but 100% reliable
- SECOND-ORDER: OCR/extraction errors create garbage nodes → pollutes graph
- MECE: Missing: audio transcription, image description (vision models)
- MAP/TERRITORY: Extracted text is lossy representation → metadata about source needed
- ADVERSARIAL: Malicious PDF (zip bomb, exploit) → need sandboxed extraction
- SIMPLICITY: Start with simple text file upload → expand to PDF later
- REVERSIBILITY: Extracted nodes can be deleted → yes

### Verdict: REJECT — YAGNI; revisit when user explicitly requests file import

---

## FINAL RANKING

| Rank | Enhancement | Darwin Score | Praxis Verdict | Priority |
|------|-------------|--------------|----------------|----------|
| 1 | Automatic Forgetting | 86.0 | STRONG ADOPT | P1 |
| 2 | User Profiles | 76.8 | ADOPT (modified) | P1 |
| 3 | Enhanced Hybrid Search | 74.5 | ADOPT (phased) | P2 |
| 4 | Contradiction Detection | 67.3 | ADOPT (manual first) | P2 |
| 5 | Connectors | 56.8 | DEFER | P3 |
| 6 | Multi-modal Extractors | 48.5 | REJECT | - |

### P1 Implementation Order:
1. Automatic Forgetting (highest score, leverages existing temporal system)
2. User Profiles (builds on forgetting, provides query context)

### P2 Implementation Order:
3. Enhanced Hybrid Search (extends existing search)
4. Contradiction Detection (needs profiles + search to be useful)

### P3:
5. Connectors (revisit after core stable)

### Rejected:
6. Multi-modal Extractors (not needed at current stage)
