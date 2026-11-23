package adapter

import (
	"context"
	"errors"
	"time"

	"github.com/huncoding/payment-processor-course/internal/ledger/domain"
	"github.com/huncoding/payment-processor-course/internal/ledger/usecase"
	ledgerv1 "github.com/huncoding/payment-processor-course/proto/gen/go/ledger/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	ledgerv1.UnimplementedLedgerServiceServer
	authorizer *usecase.Authorizer
}

func NewServer(authorizer *usecase.Authorizer) *Server {
	return &Server{authorizer: authorizer}
}

func (s Server) Authorize(ctx context.Context, request *ledgerv1.AuthorizeRequest) (*ledgerv1.AuthorizeResponse, error) {
	start := time.Now()
	charge := domain.Charge{
		IdempotencyKey:   request.GetIdempotencyKey(),
		AccountID:        request.GetAccountId(),
		CardFingerprint:  request.GetCardFingerprint(),
		AmountCents:      request.GetAmountCents(),
		Currency:         request.GetCurrency(),
		ClientIP:         request.GetClientIp(),
		MerchantCategory: request.GetMerchantCategory(),
	}

	result, err := s.authorizer.Authorize(ctx, charge)
	if err != nil {
		return nil, mapError(err)
	}

	authorizeDuration.WithLabelValues().Observe(float64(time.Since(start).Seconds()))
	countDecision(result.Decision.Status, result.Replayed, charge.AmountCents)

	return &ledgerv1.AuthorizeResponse{
		PaymentId: result.PaymentID,
		Status:    statusToProto(result.Decision.Status),
		RiskScore: result.Decision.RiskScore,
		Reasons:   result.Decision.Reasons,
		Replayed:  result.Replayed,
	}, nil
}

func (s Server) GetAccountHistory(ctx context.Context, request *ledgerv1.GetAccountHistoryRequest) (*ledgerv1.GetAccountHistoryResponse, error) {
	entries, err := s.authorizer.History(ctx, request.GetAccountId(), int(request.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, "history lookup failed")
	}

	out := make([]*ledgerv1.HistoryEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &ledgerv1.HistoryEntry{
			PaymentId:        e.PaymentID,
			AmountCents:      e.AmountCents,
			Currency:         e.Currency,
			Status:           e.Status,
			MerchantCategory: e.MerchantCategory,
			ClientIp:         e.ClientIP,
			OccurredAtUnixMs: e.OccurredAt.UnixMilli(),
		})
	}

	return &ledgerv1.GetAccountHistoryResponse{Entries: out}, nil
}

func (s Server) ListReviews(ctx context.Context, request *ledgerv1.ListReviewsRequest) (*ledgerv1.ListReviewsResponse, error) {
	reviews, err := s.authorizer.Reviews(ctx, request.GetStatus(), int(request.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	out := make([]*ledgerv1.Review, 0, len(reviews))
	for _, r := range reviews {
		out = append(out, &ledgerv1.Review{
			PaymentId:        r.PaymentID,
			Status:           r.Status,
			Report:           r.Report,
			Recommendation:   r.Recommendation,
			Decision:         r.Decision,
			DecidedBy:        r.DecidedBy,
			AmountCents:      r.AmountCents,
			Currency:         r.Currency,
			MerchantCategory: r.MerchantCategory,
			RiskScore:        r.RiskScore,
			CreatedAtUnixMs:  r.CreatedAt.UnixMilli(),
			UpdatedAtUnixMs:  r.UpdatedAt.UnixMilli(),
		})
	}

	return &ledgerv1.ListReviewsResponse{Reviews: out}, nil
}

func (s Server) DecideReview(ctx context.Context, request *ledgerv1.DecideReviewRequest) (*ledgerv1.DecideReviewResponse, error) {
	result, err := s.authorizer.DecideReview(ctx, request.GetPaymentId(), request.GetDecision(), request.GetOperator())
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidDecision):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, usecase.ErrReviewNotFound):
			return nil, status.Error(codes.NotFound, err.Error())
		case errors.Is(err, usecase.ErrReviewAlreadyDecided):
			return nil, status.Error(codes.AlreadyExists, err.Error())
		default:
			return nil, status.Error(codes.Internal, "decision failed")
		}
	}

	countReviewDecision(result.Status, result.FollowedRecommendation)

	return &ledgerv1.DecideReviewResponse{
		PaymentId:              result.PaymentID,
		Status:                 result.Status,
		FollowedRecommendation: result.FollowedRecommendation,
	}, nil
}

func (s Server) GetRecentPayments(ctx context.Context, request *ledgerv1.GetRecentPaymentsRequest) (*ledgerv1.GetRecentPaymentsResponse, error) {
	payments, err := s.authorizer.Recent(ctx, int(request.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, "recent payments lookup failed")
	}

	out := make([]*ledgerv1.RecentPayment, 0, len(payments))
	for _, p := range payments {
		out = append(out, &ledgerv1.RecentPayment{
			PaymentId:        p.PaymentID,
			AmountCents:      p.AmountCents,
			Currency:         p.Currency,
			Status:           p.Status,
			RiskScore:        p.RiskScore,
			Reasons:          p.Reasons,
			MerchantCategory: p.MerchantCategory,
			CreatedAtUnixMs:  p.CreatedAt.UnixMilli(),
		})
	}

	return &ledgerv1.GetRecentPaymentsResponse{Payments: out}, nil
}

func statusToProto(s string) ledgerv1.PaymentStatus {
	switch s {
	case domain.StatusApproved:
		return ledgerv1.PaymentStatus_PAYMENT_STATUS_APPROVED
	case domain.StatusDenied:
		return ledgerv1.PaymentStatus_PAYMENT_STATUS_DENIED
	case domain.StatusManualReview:
		return ledgerv1.PaymentStatus_PAYMENT_STATUS_MANUAL_REVIEW
	default:
		return ledgerv1.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED
	}
}

func mapError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrMissingAccount),
		errors.Is(err, domain.ErrMissingIdempotencyKey):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.Internal, "authorization failed")
	}
}
