package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/huncoding/payment-processor-course/internal/ledger/domain"
)

type Authorizer struct {
	repo      Repository
	fraud     FraudScore
	processor PaymentProcessor
	logger    *slog.Logger

	fraudTimeout time.Duration
}

func NewAuthorizer(repo Repository, fraud FraudScore, proc PaymentProcessor, logger *slog.Logger) *Authorizer {
	return &Authorizer{
		repo:         repo,
		fraud:        fraud,
		processor:    proc,
		logger:       logger,
		fraudTimeout: 100 * time.Millisecond,
	}
}

// FraudScore is the synchronous fraud check port.
type FraudScore interface {
	Score(ctx context.Context, paymentID string, c domain.Charge) (domain.Decision, error)
}

// PaymentProcessor is the external PSP port. Implementations must be idempotent on their side keyed by
// payment id.
type PaymentProcessor interface {
	Charge(ctx context.Context, paymentID string, c domain.Charge) error
}

type Repository interface {
	FindIdempotency(ctx context.Context, key, requestHash string) (result domain.Result, found bool, conflict bool, err error)
	PersistDecision(ctx context.Context, cmd PersistCommand) error
	AccountHistory(ctx context.Context, accountID string, limit int) ([]HistoryEntry, error)
	ListReviews(ctx context.Context, status string, limit int) ([]ReviewEntry, error)
	RecentPayments(ctx context.Context, limit int) ([]PaymentEntry, error)

	DecisionRepository
}

// HistoryEntry is read model row for account history.
type HistoryEntry struct {
	PaymentID        string
	AmountCents      int64
	Currency         string
	Status           string
	MerchantCategory string
	ClientIP         string
	OccurredAt       time.Time
}

// PersistCommand carries everything the repository needs to commit.
type PersistCommand struct {
	PaymentID   string
	Charge      domain.Charge
	Decision    domain.Decision
	Entries     []domain.Entry
	RequestHash string
}

// PaymentEntry is a read model row for payment attempts.
type PaymentEntry struct {
	PaymentID        string
	AccountID        string
	AmountCents      int64
	Currency         string
	Status           string
	RiskScore        float64
	Reasons          []string
	MerchantCategory string
	CreatedAt        time.Time
}

// ReviewEntry is a read model row for payment reviews.
type ReviewEntry struct {
	PaymentID        string
	AccountID        string
	AmountCents      int64
	Currency         string
	Status           string
	Report           string
	Recommendation   string
	Decision         string
	DecidedBy        string
	MerchantCategory string
	RiskScore        float64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (a *Authorizer) History(ctx context.Context, accountID string, limit int) ([]HistoryEntry, error) {
	return a.repo.AccountHistory(ctx, accountID, limit)
}

func (a *Authorizer) Reviews(ctx context.Context, status string, limit int) ([]ReviewEntry, error) {
	switch status {
	case "", "pending", "investigating", "resolved", "closed":
	default:
		return nil, fmt.Errorf("ledger: unknown review status %q", status)
	}
	return a.repo.ListReviews(ctx, status, clampLimit(limit))
}

func (a *Authorizer) Recent(ctx context.Context, limit int) ([]PaymentEntry, error) {
	return a.repo.RecentPayments(ctx, clampLimit(limit))
}

func (a *Authorizer) Authorize(ctx context.Context, charge domain.Charge) (domain.Result, error) {
	if err := charge.Validate(); err != nil {
		return domain.Result{}, err
	}

	if charge.OccurredAt.IsZero() {
		charge.OccurredAt = time.Now().UTC()
	}

	reqHash := hashCharge(charge)

	stored, found, conflict, err := a.repo.FindIdempotency(ctx, charge.IdempotencyKey, reqHash)
	if err != nil {
		return domain.Result{}, err
	}

	if found {
		stored.Replayed = true
		return stored, nil
	}

	if conflict {
		return domain.Result{}, domain.ErrIdempotencyConflict
	}

	paymentID := uuid.NewString()
	decision := a.scoreWithBudget(ctx, paymentID, charge)

	var entries []domain.Entry
	if decision.Status == domain.StatusApproved {
		if err := a.processor.Charge(ctx, paymentID, charge); err != nil {
			return domain.Result{}, fmt.Errorf("payment processor: %w", err)
		}

		entries = domain.EntriesFor(charge)

		if !domain.Balanced(entries) {
			return domain.Result{}, fmt.Errorf("payment processor: balance mismatch")
		}
	}

	cmd := PersistCommand{
		PaymentID:   paymentID,
		Charge:      charge,
		Decision:    decision,
		Entries:     entries,
		RequestHash: reqHash,
	}
	if err := a.repo.PersistDecision(ctx, cmd); err != nil {
		return domain.Result{}, fmt.Errorf("persist decision: %w", err)
	}

	return domain.Result{
		PaymentID: paymentID,
		Decision:  decision,
	}, nil
}

func (a *Authorizer) scoreWithBudget(ctx context.Context, paymentID string, c domain.Charge) domain.Decision {
	scoreCtx, cancel := context.WithTimeout(ctx, a.fraudTimeout)
	defer cancel()

	decision, err := a.fraud.Score(scoreCtx, paymentID, c)
	if err != nil {
		a.logger.Warn("fraud scorer unavailable, routing to manual review",
			slog.String("payment_id", paymentID),
			slog.String("error", err.Error()))

		return domain.Decision{
			Status:   domain.StatusManualReview,
			Reasons:  []string{"fraud_service_unavailable"},
			Degraded: true,
		}
	}

	return decision
}

func hashCharge(c domain.Charge) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%d|%s", c.AccountID, c.CardFingerprint, c.AmountCents, c.Currency)
	return hex.EncodeToString(h.Sum(nil))
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}
