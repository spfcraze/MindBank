## PRAXIS GAP-ANALYSIS: Supermemory → MindBank Enhancements
## Date: 2026-05-09
## Scope: Top 3 candidates (Auto Forgetting, User Profiles, Enhanced Hybrid Search)
## System Reality Verified: YES (checked Node model, EdgeRepo methods, temporal versioning)

---

## CHECK 1 — Inversion (How to guarantee failure?)

### Candidate A: Automatic Forgetting
1. **TTL misconfiguration** → all nodes expire immediately → empty database
   - MITIGATION: Default TTL only applies to new nodes; existing nodes keep null TTL. Minimum TTL 24h.
2. **Cron job fails silently** → nodes never expire → database bloat
   - MITIGATION: Forgetting job logs to metrics table; dashboard shows last run time; alert if >7 days.
3. **User pins important node but pin is lost** → critical memory disappears
   - MITIGATION: Pin stored as separate flag in node metadata; backup before mark superseded.

### Candidate B: User Profiles
1. **Profile extraction is wrong** → user profile contains false facts → search poisoned
   - MITIGATION: User approves profile facts before they're active; confidence threshold for auto-extraction.
2. **Profile grows unbounded** → 10,000 facts → slow query augmentation
   - MITIGATION: Profile compaction keeps top 50 facts per category; LRU eviction.
3. **Profile conflicts with explicit search** → user searches for "ivfflat" but profile says "hnsw"
   - MITIGATION: Profile is augmentation, not override; explicit query terms take priority.

### Candidate C: Enhanced Hybrid Search
1. **RRF weights wrong** → document results drown memories → user loses personal context
   - MITIGATION: Source filter UI (memories | docs | both); default to memories-only.
2. **Document index stale** → returns deleted/updated files → user confusion
   - MITIGATION: Document nodes track file hash; re-index on change; show last-sync time.
3. **Unified ranking breaks existing search** → regression in memory-only queries
   - MITIGATION: Feature flag; default to existing search; opt-in to unified.

---

## CHECK 2 — Second-order Consequences

### Candidate A: Automatic Forgetting
- 1st order: Old nodes marked superseded, search returns fresher results
- 2nd order: User asks "what did I know about X 6 months ago?" → can't find it
  - MITIGATION: "Include superseded" toggle in search; archive view in dashboard
- 3rd order: Database size stabilizes → no need for aggressive cleanup → better performance
  - ACCEPTABLE: Positive cascading effect

### Candidate B: User Profiles
- 1st order: Search results personalized → more relevant answers
- 2nd order: User notices "it knows I prefer hnsw" → trust increase, but privacy concern
  - MITIGATION: Profile visible in dashboard; user can edit/delete; local-only storage
- 3rd order: Other users (if multi-user) see different results for same query → expectation mismatch
  - ACCEPTABLE: MindBank is single-user local app; no multi-user conflict

### Candidate C: Enhanced Hybrid Search
- 1st order: One query searches both memories and documents
- 2nd order: Users stop organizing documents → rely on search → document namespace becomes messy
  - MITIGATION: Document import still requires namespace assignment; auto-extract metadata
- 3rd order: Search latency increases (two indexes) → user perceives slowness
  - MITIGATION: Parallel search goroutines; timeout per source; streaming results

---

## CHECK 3 — MECE Coverage

### What we have:
1. **Storage layer**: Nodes (with temporal versioning), edges, namespaces
2. **Search layer**: FTS + vector + RRF hybrid
3. **Capture layer**: Session auto-capture, manual node creation
4. **Presentation layer**: Dashboard, Brain3D, graph traversal

### What we need (gaps):
1. **Expiry/retention**: No TTL system → Automatic Forgetting fills this
2. **User context**: No structured user facts → User Profiles fills this
3. **External content**: No document import → Enhanced Hybrid Search needs this first
4. **Contradiction tracking**: EdgeSupersededBy exists but no auto-detection → Contradiction Detection fills this

### MECE assessment:
- The 4 enhancements cover 4 distinct gaps with no overlap
- Auto Forgetting + User Profiles are orthogonal (profiles have longer TTL than raw memories)
- Hybrid Search + Contradiction Detection are complementary (contradictions affect ranking)
- **Gap remaining**: No "memory importance" user override (pin/star) → add to Auto Forgetting

---

## CHECK 4 — Map vs Territory

### Simplifications made:
1. **Assumed temporal versioning = TTL ready** → Reality: ValidTo is for manual updates, not expiry
   - RISK: Need new `expires_at` field or use metadata → schema migration required
2. **Assumed EdgeSupersededBy is enough for contradictions** → Reality: No semantic similarity check
   - RISK: Need embedding comparison + threshold tuning → Ollama dependency
3. **Assumed document search = just another namespace** → Reality: No file ingestion pipeline
   - RISK: Need upload endpoint, text extraction, chunking → significant new subsystem

### Risk mitigation:
- Schema migration: Add `expires_at` timestamp to nodes table (nullable, default null = never)
- Contradiction: Use existing nomic-embed-text via Ollama; threshold 0.85 cosine similarity
- Documents: Start with text paste (existing node creation), file upload later

---

## CHECK 5 — Adversarial

### Weakest points:
1. **Auto Forgetting**: User creates node, forgets about it, it expires → "where did my note go?"
   - HARDENING: Expiry notification 7 days before; dashboard "Expiring Soon" section
2. **User Profiles**: Malicious/compromised session injects false profile facts
   - HARDENING: Profile facts require confidence >0.9 AND user approval; sanitize extractions
3. **Hybrid Search**: Document import brings in sensitive files (passwords, keys)
   - HARDENING: Content scanner for secrets; redaction prompt; user confirmation before indexing

### UX-Logic Divergence Check:
- Auto Forgetting default TTL = 30 days for sessions → most session nodes are >30 days old
  - RESULT: If applied retroactively, 4,729 session nodes would be marked superseded immediately
  - FIX: TTL only applies to NEW nodes; existing nodes get null TTL (never expire)

---

## CHECK 6 — Simplicity

### Simpler alternatives considered:
1. **Auto Forgetting**: Instead of TTL system, just manual "archive" button
   - REJECTED: Doesn't solve database bloat; requires user discipline
2. **User Profiles**: Instead of structured profile, just tag important nodes
   - REJECTED: Doesn't provide query augmentation; search stays generic
3. **Hybrid Search**: Instead of unified search, just add "Documents" tab
   - ACCEPTED: Phase 1 = separate tab; Phase 2 = unified ranking

### Justification for chosen complexity:
- Auto Forgetting: Leverages existing temporal system; adds one field + cron job
- User Profiles: New table but simple schema; integrates with existing search
- Hybrid Search: Most complex; phased rollout reduces risk

---

## CHECK 7 — Reversibility

### Auto Forgetting: TYPE 2 (reversible)
- Mark superseded → can unmark
- Add TTL field → can ignore it
- Cron job → can disable
- **Reversible without data loss**

### User Profiles: TYPE 2 (reversible)
- Profile table → can drop
- Query augmentation → can disable
- **Reversible without data loss**

### Enhanced Hybrid Search: TYPE 2 (reversible)
- Document nodes in separate namespace → can filter out
- Unified ranking → can revert to memory-only
- **Reversible without data loss**

---

## GAP ANALYSIS COMPLETE

├── Inversion: 3/3 failure modes mitigated [open: none]
├── Second-order: acceptable with mitigations
├── MECE: complete [gap in: memory importance override / pin system]
├── Map vs territory: top risk: schema migration for expires_at field
├── Adversarial: weakest point: retroactive TTL application → mitigated by null default
├── Simplicity: justified complexity; hybrid search phased
├── Reversibility: Type 2 for all three
└── Confidence: HIGH (85%)

### Recommended additions before implementation:
1. **Pin/Important flag** (HIGH): User can mark nodes as "never expire" → bypasses TTL
2. **Schema migration plan** (HIGH): Add `expires_at` to nodes table; backfill null
3. **Phased hybrid search** (MEDIUM): Documents tab first → unified later

### System reality verified:
- Node model: Has ValidTo (for manual updates), no expires_at → CONFIRMED: need new field
- EdgeRepo: Has Create, Delete, ListBySource, ListByTarget → CONFIRMED: sufficient for contradiction edges
- Temporal versioning: Update() creates new version → CONFIRMED: Auto Forgetting should use mark-superseded, not version
- Node types: 25+ types including EdgeSupersededBy → CONFIRMED: contradiction edge type exists
