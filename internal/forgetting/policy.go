package forgetting

import (
	"encoding/json"
	"time"

	"mindbank/internal/models"
)

// DefaultTTLs maps node types to their default TTL.
// 0 = never expires.
var DefaultTTLs = map[models.NodeType]time.Duration{
	models.NodeSession:    30 * 24 * time.Hour,
	models.NodeFact:       90 * 24 * time.Hour,
	models.NodePreference: 365 * 24 * time.Hour,
	models.NodeDecision:   180 * 24 * time.Hour,
	models.NodeProblem:    60 * 24 * time.Hour,
	models.NodeAdvice:     90 * 24 * time.Hour,
	models.NodeConcept:    120 * 24 * time.Hour,
	models.NodeQuestion:   30 * 24 * time.Hour,
	models.NodeTopic:      60 * 24 * time.Hour,
	models.NodePerson:     0,
	models.NodeProject:    0,
	models.NodeAgent:      0,
	models.NodeEvent:      30 * 24 * time.Hour,
}

// GetDefaultTTL returns the default TTL for a node type.
func GetDefaultTTL(nodeType models.NodeType) time.Duration {
	if ttl, ok := DefaultTTLs[nodeType]; ok {
		return ttl
	}
	return 90 * 24 * time.Hour // fallback
}

// GetTTL returns the effective TTL for a node (0 = never expires).
// Pinned nodes (metadata.important = true) never expire.
func GetTTL(node *models.Node) time.Duration {
	var meta map[string]interface{}
	if err := json.Unmarshal(node.Metadata, &meta); err == nil {
		if important, ok := meta["important"].(bool); ok && important {
			return 0 // never expires
		}
	}
	return GetDefaultTTL(node.NodeType)
}

// CalculateExpiry returns the expiry time for a new node.
// Returns nil if the node never expires.
func CalculateExpiry(node *models.Node) *time.Time {
	ttl := GetTTL(node)
	if ttl == 0 {
		return nil
	}
	t := time.Now().Add(ttl)
	return &t
}
