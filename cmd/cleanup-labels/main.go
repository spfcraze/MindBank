// Command cleanup-labels does a one-time pass over regex-era knowledge nodes:
// it re-labels salvageable memories (stripping markdown/list markers off
// transcript-fragment labels) and prunes worthless ones (conversational
// narration, table rows, empty-signal). Dry-run by default; --apply to write.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"mindbank/internal/db"
)

var (
	leadMarkers = regexp.MustCompile(`^[\s>#*\-]+`)
	leadNumber  = regexp.MustCompile(`^\d+[.)]\s*`)
	mdStrip     = strings.NewReplacer("**", "", "__", "", "`", "")
	alnum       = regexp.MustCompile(`[a-zA-Z0-9]`)
	// Assistant/user narration that isn't a durable memory.
	narration = regexp.MustCompile(`(?i)^(i see|i'll|i've|i have|let me|let's|now (i|let|the)|here('| i)|looking at|okay|ok[,. ]|sure|great|perfect|wait|the paste|the (user|game)|this (is|means)|so (i|the)|alright|next|good[,. ]|got it|understood|done[.,]|first[,. ]|actually)`)
)

func cleanLabel(s string) string {
	s = strings.TrimSpace(s)
	s = leadMarkers.ReplaceAllString(s, "")
	s = leadNumber.ReplaceAllString(s, "")
	s = mdStrip.Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, " :.-—")
	// Cap at ~80 chars on a word boundary, rune-safe.
	if len(s) > 80 {
		cut := s[:80]
		for len(cut) > 0 && !utf8.RuneStart(cut[len(cut)-1]) {
			cut = cut[:len(cut)-1] // back off to a rune boundary
		}
		if i := strings.LastIndex(cut, " "); i > 40 {
			cut = cut[:i]
		}
		s = strings.TrimRight(cut, " :.-—")
	}
	return strings.TrimSpace(s)
}

// midNarration catches assistant narration anywhere in the label (not just
// at the start): "…Let me debug", "…I'll fix", etc.
var midNarration = regexp.MustCompile(`(?i)(let me |let's |i'll |i see |i've |let me check|i need to |now let)`)

// classify returns "keep", "relabel:<new>", or "prune".
func classify(label, content string) string {
	cleaned := cleanLabel(label)
	// Prune: table rows or box-drawing diagram fragments.
	if strings.Count(label, "|") >= 2 || strings.ContainsAny(label, "│┌┐└┘├┤┬┴┼─") {
		return "prune"
	}
	// Prune: assistant/user narration (leading or embedded).
	if narration.MatchString(cleaned) || narration.MatchString(strings.TrimSpace(label)) || midNarration.MatchString(cleaned) {
		return "prune"
	}
	// Prune: near-empty signal after cleaning.
	if len(alnum.FindAllString(cleaned, -1)) < 5 {
		return "prune"
	}
	if cleaned != strings.TrimSpace(label) {
		return "relabel:" + cleaned
	}
	return "keep"
}

func main() {
	apply := flag.Bool("apply", false, "write changes (default: dry-run)")
	limit := flag.Int("limit", 0, "cap nodes processed (0 = all)")
	flag.Parse()

	dsn := os.Getenv("MB_DB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5434/mindbank?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	q := `SELECT id, coalesce(label,''), coalesce(content,'') FROM nodes
	      WHERE valid_to IS NULL AND node_type NOT IN ('session','event')`
	if *limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", *limit)
	}
	rows, err := pool.Query(ctx, q)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query:", err)
		os.Exit(1)
	}
	type job struct{ id, action, newLabel, orig string }
	var jobs []job
	var total, keep, relabel, prune int
	for rows.Next() {
		var id, label, content string
		if err := rows.Scan(&id, &label, &content); err != nil {
			continue
		}
		total++
		c := classify(label, content)
		switch {
		case c == "keep":
			keep++
		case c == "prune":
			prune++
			jobs = append(jobs, job{id, "prune", "", label})
		case strings.HasPrefix(c, "relabel:"):
			relabel++
			jobs = append(jobs, job{id, "relabel", strings.TrimPrefix(c, "relabel:"), label})
		}
	}
	rows.Close()

	fmt.Printf("=== label cleanup — %s ===\n", map[bool]string{true: "APPLIED", false: "DRY-RUN"}[*apply])
	fmt.Printf("examined: %d | keep: %d | relabel: %d | prune: %d\n", total, keep, relabel, prune)

	// Show a few samples of each action from a dry-run.
	if !*apply {
		var rs []string
		for _, j := range jobs {
			if j.action == "relabel" && len(rs) < 8 {
				rs = append(rs, "  "+j.newLabel)
			}
		}
		sort.Strings(rs)
		fmt.Println("--- sample relabels (new labels) ---")
		for _, s := range rs {
			fmt.Println(s)
		}
		fmt.Println("--- sample PRUNES (original labels — verify these are junk) ---")
		var ps int
		for _, j := range jobs {
			if j.action == "prune" && ps < 15 {
				fmt.Println("  ✗", strings.ReplaceAll(j.orig, "\n", " ")[:min(90, len(j.orig))])
				ps++
			}
		}
		fmt.Println("(prune = soft-delete; run with --apply to execute)")
		return
	}

	var reN, prN int64
	for _, j := range jobs {
		switch j.action {
		case "relabel":
			if tag, err := pool.Exec(ctx,
				`UPDATE nodes SET label = $2, updated_at = now() WHERE id = $1 AND valid_to IS NULL`,
				j.id, j.newLabel); err == nil {
				reN += tag.RowsAffected()
			}
		case "prune":
			// Soft-delete node + its active edges, drop embedding (mirror Delete).
			_, _ = pool.Exec(ctx, `UPDATE edges SET valid_to = now() WHERE (source_id=$1 OR target_id=$1) AND valid_to IS NULL`, j.id)
			_, _ = pool.Exec(ctx, `DELETE FROM node_embeddings WHERE node_id = $1`, j.id)
			if tag, err := pool.Exec(ctx,
				`UPDATE nodes SET valid_to = now() WHERE id = $1 AND valid_to IS NULL`, j.id); err == nil {
				prN += tag.RowsAffected()
			}
		}
	}
	fmt.Printf("relabeled: %d | pruned: %d\n", reN, prN)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
