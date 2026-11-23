package infra

import (
	"context"
	"fmt"

	"github.com/huncoding/payment-processor-course/internal/ledger/domain"
	fraudv1 "github.com/huncoding/payment-processor-course/proto/gen/go/fraud/v1"
)

type FraudClient struct {
	client fraudv1.FraudServiceClient
}

func NewFraudClient(client fraudv1.FraudServiceClient) *FraudClient {
	return &FraudClient{client: client}
}

func (f FraudClient) Score(ctx context.Context, paymentID string, c domain.Charge) (domain.Decision, error) {
	resp, err := f.client.Score(ctx, &fraudv1.ScoreRequest{
		PaymentId:        paymentID,
		AccountId:        c.AccountID,
		CardFingerprint:  c.CardFingerprint,
		AmountCents:      c.AmountCents,
		Currency:         c.Currency,
		ClientIp:         c.ClientIP,
		MerchantCategory: c.MerchantCategory,
		OccurredAtUnixMs: c.OccurredAt.UnixMilli(),
	})
	if err != nil {
		return domain.Decision{}, fmt.Errorf("fraud grpc: failed to score payment: %w", err)
	}

	status, err := verdictToStatus(resp.GetVerdict())
	if err != nil {
		return domain.Decision{}, fmt.Errorf("fraud grpc: failed to convert verdict to status: %w", err)
	}

	return domain.Decision{
		Status:    status,
		RiskScore: resp.GetRiskScore(),
		Reasons:   resp.GetReasons(),
		Degraded:  resp.GetDegraded(),
	}, nil
}

func verdictToStatus(v fraudv1.Verdict) (string, error) {
	switch v {
	case fraudv1.Verdict_VERDICT_APPROVE:
		return domain.StatusApproved, nil
	case fraudv1.Verdict_VERDICT_DENY:
		return domain.StatusDenied, nil
	case fraudv1.Verdict_VERDICT_MANUAL_REVIEW:
		return domain.StatusManualReview, nil
	default:
		return "", fmt.Errorf("fraud grpc: unknow verdict: %v", v)
	}
}
