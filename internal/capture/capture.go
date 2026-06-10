// Package capture provides RSS feed and web content ingestion for MindBank.
// Inspired by gbrain's capture pipeline.
package capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/models"
)

// Capturer provides content capture capabilities.
type Capturer struct {
	pool *pgxpool.Pool
	http *http.Client
}

// NewCapturer creates a new capture service.
func NewCapturer(pool *pgxpool.Pool) *Capturer {
	return &Capturer{
		pool: pool,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// RSSItem represents a single RSS/Atom feed entry.
type RSSItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Description string `json:"description"`
	PubDate     string `json:"pub_date"`
	GUID        string `json:"guid"`
}

// CaptureRSS fetches and parses an RSS/Atom feed, storing items as nodes.
func (c *Capturer) CaptureRSS(ctx context.Context, feedURL string) (*CaptureResult, error) {
	// Fetch feed
	resp, err := c.http.Get(feedURL)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("feed returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read feed: %w", err)
	}

	// Parse RSS items (simplified - extract basic RSS structure)
	items := parseRSSItems(string(body))

	// Store items as nodes
	stored := 0
	skipped := 0
	for _, item := range items {
		// Deduplicate by URL hash
		urlHash := sha256.Sum256([]byte(item.Link))
		hashStr := hex.EncodeToString(urlHash[:])

		// Check if already captured
		var exists bool
		err := c.pool.QueryRow(ctx, `
			SELECT EXISTS(SELECT 1 FROM nodes 
			WHERE valid_to IS NULL 
			AND metadata->>'source_url' = $1)
		`, item.Link).Scan(&exists)
		if err == nil && exists {
			skipped++
			continue
		}

		// Create node
		meta, _ := json.Marshal(map[string]interface{}{
			"source_url":   item.Link,
			"source_type":  "rss",
			"feed_url":     feedURL,
			"published":    item.PubDate,
			"url_hash":     hashStr,
			"captured_at":  time.Now().Format(time.RFC3339),
		})

		content := item.Description
		if content == "" {
			content = item.Title
		}

		_, err = c.pool.Exec(ctx, `
			INSERT INTO nodes (workspace_name, namespace, label, node_type, content, summary, metadata, importance, materialized_path)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '/' || gen_random_uuid()::text)
		`, "hermes", "global", item.Title, models.NodeFact, content, item.Title, meta, 0.5)
		if err != nil {
			continue
		}
		stored++
	}

	return &CaptureResult{
		Source:    feedURL,
		Type:      "rss",
		Items:     len(items),
		Stored:    stored,
		Skipped:   skipped,
		Timestamp: time.Now(),
	}, nil
}

// CaptureResult summarizes a capture operation.
type CaptureResult struct {
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Items     int       `json:"items_found"`
	Stored    int       `json:"stored"`
	Skipped   int       `json:"skipped"`
	Timestamp time.Time `json:"timestamp"`
}

// parseRSSItems extracts items from RSS/Atom XML (simplified parser).
func parseRSSItems(xml string) []RSSItem {
	var items []RSSItem

	// Look for <item> blocks (RSS 2.0)
	itemBlocks := splitXMLBlocks(xml, "item")
	for _, block := range itemBlocks {
		item := RSSItem{
			Title:       extractXMLTag(block, "title"),
			Link:        extractXMLTag(block, "link"),
			Description: extractXMLTag(block, "description"),
			PubDate:     extractXMLTag(block, "pubDate"),
			GUID:        extractXMLTag(block, "guid"),
		}
		if item.Link == "" {
			item.Link = item.GUID
		}
		if item.Title != "" {
			items = append(items, item)
		}
	}

	// If no items found, try Atom <entry> blocks
	if len(items) == 0 {
		entryBlocks := splitXMLBlocks(xml, "entry")
		for _, block := range entryBlocks {
			item := RSSItem{
				Title:       extractXMLTag(block, "title"),
				Link:        extractAtomLink(block),
				Description: extractXMLTag(block, "summary"),
				PubDate:     extractXMLTag(block, "updated"),
				GUID:        extractXMLTag(block, "id"),
			}
			if item.Link == "" {
				item.Link = item.GUID
			}
			if item.Title != "" {
				items = append(items, item)
			}
		}
	}

	return items
}

// splitXMLBlocks splits XML content by tag blocks.
func splitXMLBlocks(xml, tag string) []string {
	var blocks []string
	startTag := "<" + tag + ">"
	startTag2 := "<" + tag + " "
	endTag := "</" + tag + ">"

	for {
		idx := strings.Index(xml, startTag)
		idx2 := strings.Index(xml, startTag2)
		if idx == -1 || (idx2 != -1 && idx2 < idx) {
			idx = idx2
		}
		if idx == -1 {
			break
		}
		endIdx := strings.Index(xml[idx:], endTag)
		if endIdx == -1 {
			break
		}
		block := xml[idx : idx+endIdx+len(endTag)]
		blocks = append(blocks, block)
		xml = xml[idx+endIdx+len(endTag):]
	}

	return blocks
}

// extractXMLTag extracts text content from an XML tag.
func extractXMLTag(xml, tag string) string {
	startTag := "<" + tag + ">"
	startTag2 := "<" + tag + " "
	
	idx := strings.Index(xml, startTag)
	idx2 := strings.Index(xml, startTag2)
	if idx == -1 || (idx2 != -1 && idx2 < idx) {
		// Handle attributes - find closing >
		if idx2 != -1 {
			startIdx := idx2 + len(startTag2)
			closeIdx := strings.Index(xml[startIdx:], ">")
			if closeIdx != -1 {
				startTag = xml[idx2 : startIdx+closeIdx+1]
				idx = idx2
			} else {
				return ""
			}
		} else {
			return ""
		}
	}

	start := idx + len(startTag)
	endTag := "</" + tag + ">"
	end := strings.Index(xml[start:], endTag)
	if end == -1 {
		return ""
	}

	content := xml[start : start+end]
	// Unescape common XML entities
	content = strings.ReplaceAll(content, "&lt;", "<")
	content = strings.ReplaceAll(content, "&gt;", ">")
	content = strings.ReplaceAll(content, "&amp;", "&")
	content = strings.ReplaceAll(content, "&quot;", "\"")
	content = strings.ReplaceAll(content, "&#39;", "'")
	content = strings.ReplaceAll(content, "<![CDATA[", "")
	content = strings.ReplaceAll(content, "]]>", "")

	return strings.TrimSpace(content)
}

// extractAtomLink extracts href from Atom link tag.
func extractAtomLink(entry string) string {
	idx := strings.Index(entry, "<link")
	if idx == -1 {
		return ""
	}
	start := idx + 5
	closeIdx := strings.Index(entry[start:], ">")
	if closeIdx == -1 {
		return ""
	}
	tag := entry[start : start+closeIdx]
	hrefIdx := strings.Index(tag, "href=\"")
	if hrefIdx == -1 {
		return ""
	}
	hrefStart := hrefIdx + 6
	hrefEnd := strings.Index(tag[hrefStart:], "\"")
	if hrefEnd == -1 {
		return ""
	}
	return tag[hrefStart : hrefStart+hrefEnd]
}
