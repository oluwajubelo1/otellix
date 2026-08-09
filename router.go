package otellix

import (
	"fmt"
	"sort"
	"strings"
)

// SelectCheapestModel returns the cheapest model in the pricing table whose
// estimated cost for a call of ~estimatedOutputTokens fits within
// remainingBudget. When sameProviderOnly is true, only models from the same
// provider as the current model are considered.
//
// Returns an error if no model fits the budget.
func SelectCheapestModel(provider, currentModel string, estimatedOutputTokens int64, remainingBudget float64, sameProviderOnly bool) (string, error) {
	type candidate struct {
		key         string
		costPerCall float64
	}

	pricing := ListPricing()
	var candidates []candidate

	for key, entry := range pricing {
		if key == "ollama/any" {
			continue
		}
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			continue
		}
		if sameProviderOnly && parts[0] != provider {
			continue
		}
		if key == provider+"/"+currentModel {
			continue
		}
		est := float64(estimatedOutputTokens) * (entry.InputPricePerMToken + entry.OutputPricePerMToken) / 1_000_000
		if remainingBudget > 0 && est > remainingBudget {
			continue
		}
		candidates = append(candidates, candidate{key: key, costPerCall: est})
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no model found fitting budget $%.4f", remainingBudget)
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].costPerCall < candidates[j].costPerCall })
	parts := strings.SplitN(candidates[0].key, "/", 2)
	return parts[1], nil
}
