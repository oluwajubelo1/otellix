package otellix

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const maxCallRecords = 10_000

// CallRecord represents a single observed LLM call for cost tracking and analytics.
type CallRecord struct {
	Timestamp                         time.Time
	Provider, Model, FeatureID        string
	UserID, ProjectID                 string
	InputTokens, OutputTokens         int64
	CacheReadTokens, CacheWriteTokens int64
	CostUSD                           float64
	LatencyMs                         float64
}

// Global analytics tracker state.
var (
	trackerMu   sync.RWMutex
	callRecords []CallRecord
)

// recordCall adds a call record to the global tracker and prunes old records if necessary.
// Called internally after every successful Trace() call.
func recordCall(r CallRecord) {
	trackerMu.Lock()
	defer trackerMu.Unlock()

	callRecords = append(callRecords, r)
	if len(callRecords) > maxCallRecords {
		// Prune oldest records, keep the most recent maxCallRecords.
		callRecords = callRecords[len(callRecords)-maxCallRecords:]
	}
}

// ResetCallRecords clears all tracked call records.
// Intended for testing only to ensure test isolation.
func ResetCallRecords() {
	trackerMu.Lock()
	defer trackerMu.Unlock()
	callRecords = callRecords[:0]
}

// BreakdownDimension specifies how to slice cost data.
type BreakdownDimension string

const (
	ByUser     BreakdownDimension = "user"
	ByProject  BreakdownDimension = "project"
	ByFeature  BreakdownDimension = "feature"
	ByModel    BreakdownDimension = "model"
	ByProvider BreakdownDimension = "provider"
)

// BreakdownEntry represents the cost summary for a single dimension key.
type BreakdownEntry struct {
	Key          string
	TotalCostUSD float64
	CallCount    int64
	InputTokens  int64
	OutputTokens int64
}

// GetBreakdown returns cost totals grouped by the specified dimension.
// Groups are returned sorted by total cost descending.
// Empty dimension keys (e.g., empty UserID) are grouped under "" (empty string).
func GetBreakdown(dim BreakdownDimension) []BreakdownEntry {
	trackerMu.RLock()
	snapshot := make([]CallRecord, len(callRecords))
	copy(snapshot, callRecords)
	trackerMu.RUnlock()

	groups := make(map[string]*BreakdownEntry)
	for _, r := range snapshot {
		var key string
		switch dim {
		case ByUser:
			key = r.UserID
		case ByProject:
			key = r.ProjectID
		case ByFeature:
			key = r.FeatureID
		case ByModel:
			key = r.Model
		case ByProvider:
			key = r.Provider
		}

		e := groups[key]
		if e == nil {
			e = &BreakdownEntry{Key: key}
			groups[key] = e
		}
		e.TotalCostUSD += r.CostUSD
		e.CallCount++
		e.InputTokens += r.InputTokens
		e.OutputTokens += r.OutputTokens
	}

	// Convert to slice and sort by cost descending.
	out := make([]BreakdownEntry, 0, len(groups))
	for _, e := range groups {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].TotalCostUSD > out[j].TotalCostUSD
	})
	return out
}

// ForecastReport contains a monthly spend projection.
type ForecastReport struct {
	CurrentMonthSpend    float64
	ProjectedMonthSpend  float64
	DailyBurnRate        float64
	DaysElapsedInMonth   int
	DaysRemainingInMonth int
}

// GetForecast projects monthly spend based on current burn rate.
// The key parameter filters results:
//   - "" (empty): returns global forecast across all calls
//   - "user:userid": returns forecast for a specific user
//   - "project:projectid": returns forecast for a specific project
func GetForecast(key string) ForecastReport {
	trackerMu.RLock()
	snapshot := make([]CallRecord, len(callRecords))
	copy(snapshot, callRecords)
	trackerMu.RUnlock()

	now := time.Now()
	year, month, _ := now.Date()
	loc := now.Location()
	startOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	daysInMonth := daysIn(month, year)
	daysElapsed := now.Day()
	if daysElapsed < 1 {
		daysElapsed = 1
	}

	var totalCost float64
	for _, r := range snapshot {
		// Only include records from current month.
		if r.Timestamp.Before(startOfMonth) {
			continue
		}
		// Filter by key if specified.
		if key != "" {
			if strings.HasPrefix(key, "user:") {
				if r.UserID != strings.TrimPrefix(key, "user:") {
					continue
				}
			} else if strings.HasPrefix(key, "project:") {
				if r.ProjectID != strings.TrimPrefix(key, "project:") {
					continue
				}
			}
		}
		totalCost += r.CostUSD
	}

	dailyBurnRate := totalCost / float64(daysElapsed)
	projected := dailyBurnRate * float64(daysInMonth)

	return ForecastReport{
		CurrentMonthSpend:    totalCost,
		ProjectedMonthSpend:  projected,
		DailyBurnRate:        dailyBurnRate,
		DaysElapsedInMonth:   daysElapsed,
		DaysRemainingInMonth: daysInMonth - now.Day(),
	}
}

// daysIn returns the number of days in the given month of the given year.
func daysIn(m time.Month, year int) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// OptimizationSuggestion represents a model or provider switch recommendation.
type OptimizationSuggestion struct {
	CurrentProvider     string
	CurrentModel        string
	SuggestedProvider   string
	SuggestedModel      string
	EstimatedSavingsPct float64
	EstimatedSavingsUSD float64
	Reason              string
	CallCount           int64
}

// OptimizationReport contains model switch recommendations and potential savings.
type OptimizationReport struct {
	TotalSpend       float64
	PotentialSavings float64
	Suggestions      []OptimizationSuggestion
}

// GetOptimizationSuggestions analyzes call history and suggests cheaper models.
// Returns an empty report if no optimization opportunities are found.
// Only suggests models with >10% potential savings.
func GetOptimizationSuggestions() OptimizationReport {
	trackerMu.RLock()
	snapshot := make([]CallRecord, len(callRecords))
	copy(snapshot, callRecords)
	trackerMu.RUnlock()

	// Group calls by provider/model and compute average token costs.
	type group struct {
		totalCost   float64
		callCount   int64
		totalInput  int64
		totalOutput int64
	}
	groups := make(map[string]*group)
	for _, r := range snapshot {
		key := r.Provider + "/" + r.Model
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
		}
		g.totalCost += r.CostUSD
		g.callCount++
		g.totalInput += r.InputTokens
		g.totalOutput += r.OutputTokens
	}

	pricing := ListPricing()
	var suggestions []OptimizationSuggestion
	var totalSpend float64

	for key, g := range groups {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		curProvider, curModel := parts[0], parts[1]
		totalSpend += g.totalCost

		if g.callCount == 0 {
			continue
		}

		// Compute average token counts per call.
		avgInput := float64(g.totalInput) / float64(g.callCount)
		avgOutput := float64(g.totalOutput) / float64(g.callCount)

		// Get pricing for current model.
		curEntry, ok := pricing[key]
		if !ok {
			continue
		}
		curCostPerCall := (avgInput*curEntry.InputPricePerMToken + avgOutput*curEntry.OutputPricePerMToken) / 1_000_000

		if curCostPerCall <= 0 {
			continue
		}

		// Find the best (cheapest) alternative model with >10% savings.
		bestSavingsPct := 0.10
		var bestSuggestion *OptimizationSuggestion

		for altKey, altEntry := range pricing {
			// Skip the current model and Ollama wildcard (not a real alternative).
			if altKey == key || altKey == "ollama/any" {
				continue
			}

			altParts := strings.SplitN(altKey, "/", 2)
			if len(altParts) != 2 {
				continue
			}

			// Compute cost per call if we switched to this alternative.
			altCostPerCall := (avgInput*altEntry.InputPricePerMToken + avgOutput*altEntry.OutputPricePerMToken) / 1_000_000

			// Compute percentage savings.
			savingsPct := (curCostPerCall - altCostPerCall) / curCostPerCall

			// Update best if this alternative is cheaper and savings exceed threshold.
			if savingsPct > bestSavingsPct {
				bestSavingsPct = savingsPct
				savingsUSD := (curCostPerCall - altCostPerCall) * float64(g.callCount)
				s := OptimizationSuggestion{
					CurrentProvider:     curProvider,
					CurrentModel:        curModel,
					SuggestedProvider:   altParts[0],
					SuggestedModel:      altParts[1],
					EstimatedSavingsPct: savingsPct * 100,
					EstimatedSavingsUSD: savingsUSD,
					Reason:              "Lower cost per token for similar capabilities",
					CallCount:           g.callCount,
				}
				bestSuggestion = &s
			}
		}

		if bestSuggestion != nil {
			suggestions = append(suggestions, *bestSuggestion)
		}
	}

	// Sort suggestions by estimated savings (highest first).
	sort.Slice(suggestions, func(i, j int) bool {
		return suggestions[i].EstimatedSavingsUSD > suggestions[j].EstimatedSavingsUSD
	})

	// Compute total potential savings.
	var potentialSavings float64
	for _, s := range suggestions {
		potentialSavings += s.EstimatedSavingsUSD
	}

	return OptimizationReport{
		TotalSpend:       totalSpend,
		PotentialSavings: potentialSavings,
		Suggestions:      suggestions,
	}
}
