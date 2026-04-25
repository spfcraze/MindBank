# MindBank v0.4.14 Release Notes

## Release Date: 2026-04-24

---

## 🐛 Bug Fixes

### Button Clickability Fixed
- **Root cause**: `body::after` pseudo-element had `z-index:9999` covering the entire viewport, blocking click events in Firefox
- **Fixes applied**:
  - `body::after`: `z-index:9999` → `z-index:0`
  - `.container`: Added `position:relative; z-index:2` (proper stacking context)
  - `.nav`: Added `position:relative; z-index:10`
  - `.header-actions`: Added `position:relative; z-index:20`
- **Restart Server** and **Sync Sessions** buttons are now fully clickable

### DQA Trend Panel Fixed
- Fixed `TypeError: can't access property "textContent"` — DQA trend DOM elements were missing
- Added missing HTML elements: `dqa-trend-current`, `dqa-trend-arrow`, `dqa-trend-prev`, `dqa-trend-sparkline`, `dqa-trend-info`
- Fixed API endpoint path: `/api/v1/dqa/trend` → `/api/v1/nodes/dqa/trend`

### Load More Button Fixed
- Added missing `id="loadMoreBtn"` attribute to prevent null reference error

---

## ✨ New Features

### Auto-Migration on File Sync
- When syncing migration files via the Update tab, migrations now **auto-run** in the background
- When `run-migrations.sh` is synced, it automatically executes all pending migrations
- No manual intervention required — database stays in sync with code

### Enhanced Update Tab
- File sync now supports `scripts/run-migrations.sh`
- Added to tracked files list for selective sync

---

## 🗄️ Database Migrations

### New Migrations Added
| File | Description |
|------|-------------|
| `012_dqa_snapshots.sql` | DQA trend tracking table (graph health over time) |
| `055_question_node_type.sql` | Question node type support |
| `056_materialized_path.sql` | Materialized path for ancestor/descendant queries |
| `057_heal_logs.sql` | Debug self-healing audit trail |

### Migration Directory Synced
- All migrations (001-057) now available in both:
  - `internal/db/migrations/` (source)
  - `migrations/` (deployment)
  - `_ready/migrations/` (release package)

---

## 📦 Deployment

### _ready/ Folder (Complete Release Package)
The `_ready/` folder contains everything needed for deployment:
```
_ready/
├── mindbank-api              # Compiled binary
├── mindbank-mcp              # MCP server binary
├── web/dashboard/index.html  # Dashboard frontend
├── internal/handler/static/  # Served static files
├── internal/handler/updates.go
├── migrations/               # 001-057 (all SQL files)
├── scripts/
│   ├── run-migrations.sh     # Auto-run migrations
│   └── update.sh             # Full update script
└── plugins/                  # Memory provider plugins
```

---

## 🔄 Update Instructions

### For Existing Installations

1. **Pull latest code:**
   ```bash
   git pull origin master
   ```

2. **Run migrations:**
   ```bash
   ./scripts/run-migrations.sh
   ```

3. **Rebuild binary:**
   ```bash
   go build -o mindbank-api ./cmd/mindbank
   ```

4. **Restart server:**
   ```bash
   ./stop.sh && ./start.sh
   ```

### Via Update Tab (File Sync)
1. Go to **Update** tab in dashboard
2. Sync `scripts/run-migrations.sh` — migrations auto-run
3. Sync `web/dashboard/index.html` — UI updates
4. Restart server via **Restart Server** button

---

## 📝 Files Changed

- `web/dashboard/index.html` — Button clickability, DQA trend, z-index fixes
- `internal/handler/static/index.html` — Synced copy
- `internal/handler/updates.go` — Auto-migration trigger, tracked files
- `scripts/run-migrations.sh` — Migration runner
- `migrations/012_dqa_snapshots.sql` — New migration
- `migrations/057_heal_logs.sql` — New migration

---

## 🙏 Credits

Thanks to the community for reporting the button clickability issue. The root cause (z-index stacking context) was a subtle CSS bug that only manifested in certain browsers.

---

**Full Changelog**: Compare with previous release
