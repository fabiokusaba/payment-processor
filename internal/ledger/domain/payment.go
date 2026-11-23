package domain

import (
	"errors"
	"time"
)

const (
	StatusApproved     = "approved"
	StatusDenied       = "denied"
	StatusManualReview = "manual_review"
)

const (
	DirectionDebit  = "debit"
	DirectionCredit = "credit"
)

const MerchantSettlementAccount = "00000000-0000-0000-0000-000000000001"

var (
	ErrInvalidAmount         = errors.New("ledger: amount must be positive")
	ErrMissingAccount        = errors.New("ledger: account id is required")
	ErrMissingIdempotencyKey = errors.New("ledger: idempotency key is required")
	ErrIdempotencyConflict   = errors.New("ledger: idempotency key reused with different request")
)

type Decision struct {
	Status    string
	RiskScore float64
	Reasons   []string
	Degraded  bool
}

type Result struct {
	PaymentID string
	Decision  Decision
	Replayed  bool
}

type Charge struct {
	IdempotencyKey   string
	AccountID        string
	CardFingerprint  string
	AmountCents      int64
	Currency         string
	ClientIP         string
	MerchantCategory string
	OccurredAt       time.Time
}

type Entry struct {
	AccountID   string
	Direction   string
	AmountCents int64
}

func EntriesFor(c Charge) []Entry {
	return []Entry{
		{AccountID: c.AccountID, Direction: DirectionDebit, AmountCents: c.AmountCents},
		{AccountID: MerchantSettlementAccount, Direction: DirectionCredit, AmountCents: c.AmountCents},
	}
}

func Balanced(entries []Entry) bool {
	var sum int64

	for _, entry := range entries {
		if entry.Direction == DirectionDebit {
			sum += entry.AmountCents
		} else {
			sum -= entry.AmountCents
		}
	}

	return sum == 0
}

func (c *Charge) Validate() error {
	if c.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}

	if c.AccountID == "" {
		return ErrMissingAccount
	}

	if c.AmountCents <= 0 {
		return ErrInvalidAmount
	}

	return nil
}
