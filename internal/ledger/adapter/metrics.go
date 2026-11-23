package adapter

import (
	"github.com/huncoding/payment-processor-course/internal/platform/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	authorizeDuration = metrics.NewHistogram(
		"ledger_authorize_duration_seconds",
		"End to end duration of one authorization.",
	)
	decisions = metrics.NewCounter(
		"ledger_decisions_total",
		"Authorization outcomes by status and replay.",
		"status", "replayed",
	)
	amountVolume = metrics.NewCounter(
		"ledger_amount_cents_total",
		"Money volume that reached each decision in cents.",
		"status",
	)
	reviewDecisions = metrics.NewCounter(
		"ledger_review_decisions_total",
		"Human review decisions and their agreement with the agent.",
		"decision", "followed_recommendation",
	)
)

func countReviewDecision(decision string, followed bool) {
	d := "false"
	if followed {
		d = "true"
	}
	reviewDecisions.With(prometheus.Labels{"decision": decision, "followed_recommendation": d}).Inc()
}

func countDecision(status string, replayed bool, amountCents int64) {
	d := "false"
	if replayed {
		d = "true"
	}
	decisions.With(prometheus.Labels{"status": status, "replayed": d}).Inc()
	if !replayed {
		amountVolume.With(prometheus.Labels{"status": status}).Add(float64(amountCents))
	}
}
