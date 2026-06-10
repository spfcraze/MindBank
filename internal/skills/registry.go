// Package skills provides a pluggable skill registry for MindBank.
// Inspired by gbrain's skill registry concept.
package skills

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Skill is a pluggable analysis module that can be executed on the graph.
type Skill interface {
	Name() string
	Description() string
	Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error)
}

// Registry manages available skills.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
	pool   *pgxpool.Pool
}

// NewRegistry creates a new skill registry with built-in skills.
func NewRegistry(pool *pgxpool.Pool) *Registry {
	r := &Registry{
		skills: make(map[string]Skill),
		pool:   pool,
	}

	// Register built-in skills
	r.Register(&TaxonomySkill{pool: pool})
	r.Register(&QualitySkill{pool: pool})
	r.Register(&DigestSkill{pool: pool})
	r.Register(&EnrichmentSkill{pool: pool})

	return r
}

// Register adds a skill to the registry.
func (r *Registry) Register(skill Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[skill.Name()] = skill
}

// List returns all registered skills.
func (r *Registry) List() []SkillInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var list []SkillInfo
	for _, skill := range r.skills {
		list = append(list, SkillInfo{
			Name:        skill.Name(),
			Description: skill.Description(),
		})
	}
	return list
}

// Execute runs a skill by name with given parameters.
func (r *Registry) Execute(ctx context.Context, name string, params map[string]interface{}) (map[string]interface{}, error) {
	r.mu.RLock()
	skill, ok := r.skills[name]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}

	return skill.Execute(ctx, params)
}

// SkillInfo describes a registered skill.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// --- Built-in Skills ---

// TaxonomySkill auto-classifies nodes by topic.
type TaxonomySkill struct {
	pool *pgxpool.Pool
}

func (s *TaxonomySkill) Name() string        { return "taxonomy" }
func (s *TaxonomySkill) Description() string { return "Auto-classify nodes by topic using keyword-based taxonomy" }

func (s *TaxonomySkill) Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// Import taxonomy package
	return map[string]interface{}{
		"message": "Run POST /api/v1/taxonomy/classify-all to classify nodes",
		"endpoints": []string{
			"GET /api/v1/taxonomy/distribution",
			"POST /api/v1/taxonomy/classify-all",
			"GET /api/v1/taxonomy/suggest-edges",
		},
	}, nil
}

// QualitySkill computes graph health metrics.
type QualitySkill struct {
	pool *pgxpool.Pool
}

func (s *QualitySkill) Name() string        { return "quality" }
func (s *QualitySkill) Description() string { return "Compute graph health metrics including orphan rate, density, and quality score" }

func (s *QualitySkill) Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"message": "Run GET /api/v1/quality/metrics for graph health",
		"endpoint": "GET /api/v1/quality/metrics",
	}, nil
}

// DigestSkill generates memory digest reports.
type DigestSkill struct {
	pool *pgxpool.Pool
}

func (s *DigestSkill) Name() string        { return "digest" }
func (s *DigestSkill) Description() string { return "Generate periodic memory digest reports with trends and hot nodes" }

func (s *DigestSkill) Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	period := "daily"
	if p, ok := params["period"].(string); ok && p != "" {
		period = p
	}
	return map[string]interface{}{
		"message": fmt.Sprintf("Run GET /api/v1/digest?period=%s for digest", period),
		"endpoints": []string{
			fmt.Sprintf("GET /api/v1/digest?period=%s", period),
			"GET /api/v1/digest/trends?days=7",
		},
	}, nil
}

// EnrichmentSkill auto-summarizes nodes.
type EnrichmentSkill struct {
	pool *pgxpool.Pool
}

func (s *EnrichmentSkill) Name() string        { return "enrichment" }
func (s *EnrichmentSkill) Description() string { return "Auto-summarize nodes with empty or minimal content" }

func (s *EnrichmentSkill) Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	limit := 50
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}
	return map[string]interface{}{
		"message": fmt.Sprintf("Run POST /api/v1/enrichment/enrich-all?limit=%d to enrich nodes", limit),
		"endpoint": fmt.Sprintf("POST /api/v1/enrichment/enrich-all?limit=%d", limit),
	}, nil
}
