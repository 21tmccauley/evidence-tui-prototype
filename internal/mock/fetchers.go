package mock

import (
	"fmt"
	"sync"
	"time"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

type FetcherID = runner.FetcherID

type BehaviorKind int

const (
	BehaviorNormal BehaviorKind = iota
	BehaviorStreaming
	BehaviorSlowStart
	BehaviorStall
	BehaviorPartial
	BehaviorHardFail
	BehaviorQuick
)

// Fetcher is catalog identity plus mock demo knobs (Behavior, EstDuration).
type Fetcher struct {
	ID          FetcherID
	Source      string
	Name        string
	Description string
	Tags        []string
	EstDuration time.Duration
	Behavior    BehaviorKind
}

// Demo behaviors per id; TestBehaviorOverridesPointAtKnownIDs guards keys against the embedded catalog.
var behaviorOverrides = map[string]BehaviorKind{
	"EVD-KMS-ROT":             BehaviorQuick,
	"EVD-IAM-POLICIES":        BehaviorPartial,
	"EVD-CLOUDTRAIL-CONFIG":   BehaviorStreaming,
	"EVD-EKS-PRIV":            BehaviorStreaming,
	"EVD-WAF-ALL-RULES":       BehaviorHardFail,
	"EVD-SSLLABS-TLS-SCAN":    BehaviorSlowStart,
	"EVD-CHECKOV-TERRAFORM":   BehaviorStreaming,
	"EVD-CHECKOV-KUBERNETES":  BehaviorStreaming,
	"EVD-GUARD-DUTY":          BehaviorSlowStart,
	"EVD-BACKUP-VALIDATION":   BehaviorPartial,
	"EVD-DETECT-NEW-RESOURCE": BehaviorSlowStart,
}

var durationOverrides = map[string]time.Duration{
	"EVD-SSLLABS-TLS-SCAN":    8 * time.Second,
	"EVD-CHECKOV-TERRAFORM":   7 * time.Second,
	"EVD-CHECKOV-KUBERNETES":  7 * time.Second,
	"EVD-CLOUDTRAIL-CONFIG":   6 * time.Second,
	"EVD-EKS-PRIV":            6 * time.Second,
	"EVD-DETECT-NEW-RESOURCE": 6 * time.Second,
}

const defaultEstDuration = 4 * time.Second

// Embedded or --catalog override, cached for mock runner and screens.
var (
	catMu       sync.Mutex
	catOverride string
	catCache    []Fetcher
)

// SetCatalogOverride sets the `--catalog` JSON path, or "" for embedded.
func SetCatalogOverride(path string) {
	catMu.Lock()
	defer catMu.Unlock()
	catOverride = path
	catCache = nil
}

// Catalog returns cached fetchers (panics on error; call EnsureCatalog first for a clean exit path).
func Catalog() []Fetcher {
	if err := EnsureCatalog(); err != nil {
		panic(fmt.Errorf("load catalog: %w", err))
	}
	catMu.Lock()
	defer catMu.Unlock()
	return catCache
}

// EnsureCatalog loads and caches the catalog (embedded or override); idempotent.
func EnsureCatalog() error {
	catMu.Lock()
	defer catMu.Unlock()
	if catCache != nil {
		return nil
	}
	var (
		scripts []catalog.Script
		err     error
	)
	if catOverride != "" {
		_, scripts, err = catalog.LoadFile(catOverride)
	} else {
		_, scripts, err = catalog.LoadEmbedded()
	}
	if err != nil {
		return err
	}
	out := make([]Fetcher, 0, len(scripts))
	for _, s := range scripts {
		out = append(out, Fetcher{
			ID:          runner.FetcherID(s.ID),
			Source:      s.Source,
			Name:        s.Name,
			Description: s.Description,
			Tags:        s.Tags,
			EstDuration: durationFor(s.ID),
			Behavior:    behaviorFor(s.ID),
		})
	}
	catCache = out
	return nil
}

func behaviorFor(id string) BehaviorKind {
	if b, ok := behaviorOverrides[id]; ok {
		return b
	}
	return BehaviorNormal
}

func durationFor(id string) time.Duration {
	if d, ok := durationOverrides[id]; ok {
		return d
	}
	return defaultEstDuration
}

// Sources returns the unique source identifiers in catalog order.
func Sources(cat []Fetcher) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, f := range cat {
		if !seen[f.Source] {
			seen[f.Source] = true
			out = append(out, f.Source)
		}
	}
	return out
}

// CountBySource returns how many fetchers each source has.
func CountBySource(cat []Fetcher) map[string]int {
	out := map[string]int{}
	for _, f := range cat {
		out[f.Source]++
	}
	return out
}
