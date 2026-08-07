// Command backfill-ns re-derives namespaces for existing "global" nodes.
//
// Session nodes get their namespace from their stored content (same project
// extractor used at ingest); knowledge nodes inherit the namespace of the
// session that produced them. Namespace is a metadata field, so this does a
// direct UPDATE rather than spawning a temporal version per node.
//
// Dry-run by default — pass --apply to write. --limit caps rows for testing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"mindbank/internal/autocapture"
	"mindbank/internal/db"
)

func main() {
	apply := flag.Bool("apply", false, "actually write changes (default: dry-run)")
	limit := flag.Int("limit", 0, "max session nodes to process (0 = all)")
	flag.Parse()

	dsn := os.Getenv("MB_DB_DSN")
	if dsn == "" {
		dsn = "postgres://mindbank:mindbank@localhost:5436/mindbank?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := db.Connect(dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer pool.Close()

	// 1) Session nodes currently in global. Re-derive namespace from the
	//    ORIGINAL session file (metadata.source_file) when it still exists —
	//    the stored node content only kept truncated assistant text and lost
	//    the project-path signal. Fall back to stored content.
	q := `SELECT id, coalesce(content,''), coalesce(metadata->>'source_file','') FROM nodes
	      WHERE valid_to IS NULL AND namespace = 'global' AND node_type = 'session'`
	if *limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", *limit)
	}
	rows, err := pool.Query(ctx, q)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query sessions:", err)
		os.Exit(1)
	}
	type sess struct{ id, ns string }
	var sessions []sess
	var fromFile, fromContent int
	for rows.Next() {
		var id, content, srcFile string
		if err := rows.Scan(&id, &content, &srcFile); err != nil {
			continue
		}
		ns := "global"
		if srcFile != "" {
			if data, err := os.ReadFile(srcFile); err == nil {
				if n := autocapture.NamespaceFromSession(data); n != "global" {
					ns = n
					fromFile++
				}
			}
		}
		if ns == "global" {
			if n := autocapture.NamespaceFromSession([]byte(content)); n != "global" {
				ns = n
				fromContent++
			}
		}
		sessions = append(sessions, sess{id, ns})
	}
	rows.Close()
	fmt.Printf("resolved from source file: %d | from stored content: %d\n", fromFile, fromContent)

	preview := map[string]int{}
	sessionMoved := 0
	for _, s := range sessions {
		preview[s.ns]++
		if s.ns != "global" {
			sessionMoved++
		}
	}

	// 2) Knowledge nodes inherit their producing session's namespace.
	//    (edge: session --produced--> knowledge)
	var knowledgeMoved int64
	for _, s := range sessions {
		if s.ns == "global" {
			continue
		}
		if *apply {
			tag, err := pool.Exec(ctx, `
				UPDATE nodes SET namespace = $1, updated_at = now()
				WHERE valid_to IS NULL AND namespace = 'global'
				  AND id IN (SELECT target_id FROM edges WHERE source_id = $2 AND edge_type = 'produced')
			`, s.ns, s.id)
			if err == nil {
				knowledgeMoved += tag.RowsAffected()
			}
			if _, err := pool.Exec(ctx, `UPDATE nodes SET namespace=$1, updated_at=now() WHERE id=$2 AND valid_to IS NULL`, s.ns, s.id); err != nil {
				fmt.Fprintln(os.Stderr, "update session", s.id, err)
			}
		} else {
			var cnt int64
			pool.QueryRow(ctx, `SELECT count(*) FROM nodes
				WHERE valid_to IS NULL AND namespace='global'
				  AND id IN (SELECT target_id FROM edges WHERE source_id=$1 AND edge_type='produced')`, s.id).Scan(&cnt)
			knowledgeMoved += cnt
		}
	}

	// Report
	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range preview {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })

	mode := "DRY-RUN (no changes written)"
	if *apply {
		mode = "APPLIED"
	}
	fmt.Println("=== namespace backfill —", mode, "===")
	fmt.Printf("session nodes examined: %d\n", len(sessions))
	fmt.Printf("session nodes re-namespaced: %d\n", sessionMoved)
	fmt.Printf("knowledge nodes re-namespaced (via produced edges): %d\n", knowledgeMoved)
	fmt.Println("--- resulting session namespaces (top 15) ---")
	for i, p := range top {
		if i >= 15 {
			break
		}
		fmt.Printf("%6d  %s\n", p.v, p.k)
	}
}
