package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/huncoding/payment-processor-course/internal/ledger/domain"
)

var (
	ErrReviewNotFound       = errors.New("ledger: review not found")
	ErrReviewAlreadyDecided = errors.New("ledger: review already decided")
	ErrInvalidDecision      = errors.New("ledger: decision must be approved or denied")
)

type ReviewCase struct {
	PaymentID        string
	ReviewStatus     string
	Recommendation   string
	AccountID        string
	CardFingerprint  string
	AmountCents      int64
	Currency         string
	MerchantCategory string
	ClientIP         string
}

type DecisionCommand struct {
	PaymentID string
	EventID   string
	Decision  string
	Operator  string
	Entries   []domain.Entry
	DecidedAt time.Time
}

type DecisionResult struct {
	PaymentID              string
	Status                 string
	FollowedRecommendation bool
}

type DecisionRepository interface {
	ReviewCase(ctx context.Context, paymentID string) (c ReviewCase, found bool, err error)
	ApplyDecision(ctx context.Context, cmd DecisionCommand) (applied bool, err error)
}

func (a *Authorizer) DecideReview(ctx context.Context, paymentID, decision, operator string) (DecisionResult, error) {
	if decision != domain.StatusApproved && decision != domain.StatusDenied {
		return DecisionResult{}, ErrInvalidDecision
	}

	if operator == "" {
		operator = "unknow"
	}

	c, found, err := a.repo.ReviewCase(ctx, paymentID)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("load review: %w", err)
	}
	if !found {
		return DecisionResult{}, ErrReviewNotFound
	}
	if c.ReviewStatus == "closed" {
		return DecisionResult{}, ErrReviewAlreadyDecided
	}

	charge := domain.Charge{
		AccountID:        c.AccountID,
		AmountCents:      c.AmountCents,
		Currency:         c.Currency,
		MerchantCategory: c.MerchantCategory,
		ClientIP:         c.ClientIP,
		CardFingerprint:  c.CardFingerprint,
	}

	var entries []domain.Entry
	if decision == domain.StatusApproved {
		if err := a.processor.Charge(ctx, paymentID, charge); err != nil {
			return DecisionResult{}, fmt.Errorf("payment processor: %w", err)
		}
		entries = domain.EntriesFor(charge)
		if !domain.Balanced(entries) {
			return DecisionResult{}, fmt.Errorf("payment processor: balance mismatch")
		}
	}

	applied, err := a.repo.ApplyDecision(ctx, DecisionCommand{
		PaymentID: paymentID,
		Decision:  decision,
		Operator:  operator,
		Entries:   entries,
		DecidedAt: time.Now().UTC(),
	})
	if err != nil {
		return DecisionResult{}, fmt.Errorf("apply decision: %w", err)
	}
	if !applied {
		return DecisionResult{}, ErrReviewAlreadyDecided
	}

	a.logger.Info("review decided",
		slog.String("payment_id", paymentID),
		slog.String("decision", decision),
		slog.String("operator", operator),
		slog.String("recommendation", c.Recommendation))

	return DecisionResult{
		PaymentID:              paymentID,
		Status:                 decision,
		FollowedRecommendation: decision == recommendationToStatus(c.Recommendation),
	}, nil
}

func recommendationToStatus(r string) string {
	switch r {
	case "approve":
		return domain.StatusApproved
	case "deny":
		return domain.StatusDenied
	default:
		return ""
	}
}
