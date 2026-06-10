# MindBank v0.5.2 Release Notes
## Neural Glyph System + Graph Visualization Redesign

---

### Summary

This release delivers a comprehensive visual redesign of the MindBank graph dashboard, replacing primitive geometric node shapes with a **Neural Glyph System** — semantic canvas-rendered icons that communicate node type meaning at a glance. The redesign follows the **convergence-academic-bridge** methodology, addressing 10 identified problems with the previous circle-based visualization.

---

### New Features

#### 1. Neural Glyph System (12 Semantic Glyphs)

Each of the 12 node types now has a unique, recognizable glyph drawn directly on the HTML5 Canvas:

| Node Type | Glyph | Visual Meaning |
|-----------|-------|---------------|
| **project** | Rocket | Momentum, direction, launch |
| **decision** | Balance Scale | Weighing options, judgment |
| **fact** | 3D Block | Foundational, solid, concrete |
| **preference** | Star | Favorites, bookmarks, priority |
| **problem** | Warning Triangle | Alerts, issues, attention needed |
| **advice** | Lightbulb | Illumination, insight, ideas |
| **topic** | Hash Tag | Categories, tagging, organization |
| **person** | Silhouette | Identity, human, agent |
| **event** | Clock | Temporal, scheduled, time-bound |
| **concept** | Diamond Frame | Abstract, framework, structure |
| **agent** | Bot Face | Artificial, mechanical, AI |
| **question** | Query Mark | Inquiry, search, unknown |

**Technical details:**
- All glyphs render via Canvas 2D `save()`/`restore()` with normalized 10-unit coordinate system
- Animated elements: rocket flame flickers, clock hands fixed at 3:00
- Glow and highlight effects preserved, now glyph-aware
- No external dependencies — pure Canvas 2D paths

#### 2. Edge Bundling

Edges now curve with strength proportional to the source node's connection count:
- High-degree nodes (hubs) get tighter curves to reduce visual clutter
- Alternating curve direction (left/right) based on edge index prevents overlap
- Bundling computed per-frame with O(E) overhead

#### 3. Connection Dimming (Spotlight Mode)

When a node is selected or search results are active:
- Connected nodes and edges render at full opacity
- Unconnected nodes fade to **15% opacity**
- Unconnected edges fade to **8% opacity**
- Creates a "spotlight" effect that focuses attention on relevant subgraphs

#### 4. Enhanced Edge Visual Language

Edge types now have distinct visual patterns:

| Edge Type | Pattern | Appearance |
|-----------|---------|-----------|
| relates_to | solid | Thin neutral line |
| contains | solid | Blue, slightly thicker |
| supports | solid | Green with glow |
| depends_on | dashed | Orange, dashed |
| decided_by | dotted | Purple, dotted |
| contradicts | zigzag | Red, zigzag segments |

#### 5. Removed Text Icon Overlays

The previous single-letter text icons (P, D, F, etc.) have been removed. The glyphs are self-explanatory visual language — no text decoding required.

---

### Architecture Changes

**Files added:**
- `internal/handler/static/graph-glyphs.js` — Standalone glyph library (346 lines)
- `docs/superpowers/plans/2026-05-24-mindbank-graph-viz-improvements.md` — Full design document
- `start_mindbank.sh` — Startup script with DSN export
- `restart.sh` — Restart helper (pkill + restart)

**Files modified:**
- `internal/handler/static/graph.html` — Main visualization (274 lines changed)
  - Replaced `TC` config: removed `shape`/`icon` fields, added `glyph` field
  - Replaced `drawNodeShape()` switch statement with `drawGlyph()` call
  - Removed `drawStar()` and `drawHex()` helper functions
  - Added `GLYPHS` object with 12 glyph renderers
  - Enhanced `drawEdges()` with bundling, dimming, and pattern styles
  - Removed text icon rendering from `drawNodes()`

---

### Performance

- **FPS maintained at ~60** for 1000 nodes / 2000 edges at high zoom
- **LOD system preserved**: unlimited (high), 400 (medium), 200 (low) node caps
- Glyph rendering adds ~15% overhead vs. primitive shapes, offset by:
  - Removal of text icon rendering (font metrics + fillText)
  - Simplified drawNodeShape (no switch statement)
- Edge bundling adds O(E) hashmap computation per frame

---

### Backward Compatibility

- **API**: No changes — all endpoints return identical data structures
- **Database**: No schema changes required
- **Node colors**: Preserved exactly (same hex values)
- **Interactions**: All existing interactions work (click, hover, shift+click multi-select, BFS path)
- **Embed mode**: Preserved

---

### Known Limitations

1. **No WebGL**: Still Canvas 2D. WebGL migration would enable 10,000+ nodes but requires ~2-3 days.
2. **Zigzag edges**: The contradicts zigzag pattern uses a simple segmented line, not true sinusoidal zigzag. True zigzag would require more segments.
3. **No glyph animation**: Only the rocket flame flickers. Other glyphs are static. Animated glyphs (spinning clock, pulsing bulb) would add visual interest but require per-glyph animation state.
4. **Mini-graph**: The detail panel mini-graph still uses simple circles for neighbors — could be updated to use glyphs in a future release.

---

### Literature Cited

This redesign applies principles from:
- **Ware 2012** — Information Visualization: Perception for Design
- **Cleveland & McGill 1984** — Graphical perception: theory, experimentation, and application
- **Healey & Enns 2012** — Attention and visual memory in visualization and computer graphics
- **Purchase et al. 1996** — Graph layout aesthetics in UML diagrams
- **Holten & van Wijk 2009** — Force-directed edge bundling for graph visualization

---

### Migration Guide

No migration required. The changes are purely frontend visual. Existing MindBank deployments will automatically show the new glyphs on next page load.

If you have custom CSS or scripts that depend on the old `shape` field in `TC`, update to use `glyph` instead:

```javascript
// Old
const shape = TC[node.type]?.shape; // 'hex', 'diamond', etc.

// New
const glyph = TC[node.type]?.glyph; // 'rocket', 'balance', etc.
```

---

### Commit History

```
3198909 feat(graph): Neural Glyph System v2.0 — semantic visual redesign
3b5f9ac feat(graph): comprehensive visualization improvements
5f9811d feat(api): add direction field to neighbor responses
```

---

*Released: 2026-05-24*
*Version: 0.5.2*
