package ai

import (
	"context"
	"log"
	"time"

	"durooma/internal/models"
)

type backgroundStore interface {
	DueCategorization(context.Context, int) ([]models.Transaction, error)
	DeferCategorization(context.Context, []int64) error
}

// RunBackground continuously drains the database queue. No browser or import
// hook is needed; unfinished work is rediscovered after a server restart.
func RunBackground(ctx context.Context, st backgroundStore, svc *Service, poll time.Duration) {
	for ctx.Err() == nil {
		txns, err := st.DueCategorization(ctx, svc.batchSize)
		if err == nil && len(txns) > 0 {
			var rep Report
			rep, err = svc.Categorize(ctx, txns)
			if ctx.Err() != nil {
				return
			}
			ids := make([]int64, len(txns))
			for i, t := range txns {
				ids[i] = t.ID
			}
			if deferErr := st.DeferCategorization(ctx, ids); deferErr != nil {
				log.Printf("background categorization: persist retry: %v", deferErr)
			}
			log.Printf("background categorization: %d by rules, %d by AI, %d unresolved", rep.ByRules, rep.ByAI, rep.Unresolved)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("background categorization: %v", err)
		}
		if !wait(ctx, poll) {
			return
		}
	}
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
