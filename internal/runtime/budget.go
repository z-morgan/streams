package runtime

import "context"

// BudgetRuntime wraps a Runtime to inject max-budget-usd into every request.
type BudgetRuntime struct {
	Inner     Runtime
	MaxBudget string
}

func (b *BudgetRuntime) Run(ctx context.Context, req Request) (*Response, error) {
	if req.Options == nil {
		req.Options = make(map[string]string)
	}
	req.Options["maxBudgetUsd"] = b.MaxBudget
	return b.Inner.Run(ctx, req)
}
