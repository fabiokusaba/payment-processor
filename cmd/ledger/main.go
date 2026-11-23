package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huncoding/payment-processor-course/internal/ledger/adapter"
	"github.com/huncoding/payment-processor-course/internal/ledger/infra"
	"github.com/huncoding/payment-processor-course/internal/ledger/usecase"
	"github.com/huncoding/payment-processor-course/internal/platform/config"
	"github.com/huncoding/payment-processor-course/internal/platform/kafka"
	"github.com/huncoding/payment-processor-course/internal/platform/logging"
	"github.com/huncoding/payment-processor-course/internal/platform/metrics"
	"github.com/huncoding/payment-processor-course/internal/platform/postgres"
	fraudv1 "github.com/huncoding/payment-processor-course/proto/gen/go/fraud/v1"
	ledgerv1 "github.com/huncoding/payment-processor-course/proto/gen/go/ledger/v1"
	kafkago "github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logger := logging.New("ledger")

	if err := run(logger); err != nil {
		logger.Error("service stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	grpcAddr := config.String("GRPC_ADDR", "9001")
	metricsAddr := config.String("METRICS_ADDR", "9101")
	databaseURL := config.String("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/payments?sslmode=disable")
	fraudAddr := config.String("FRAUD_GRPC_ADDR", "localhost:9002")
	kafkaBroker := config.String("KAFKA_BROKER", "localhost:9092")
	stripeKey := config.String("STRIPE_API_KEY", "")

	go metrics.Serve(ctx, metricsAddr, logger)

	if err := infra.Migrate(ctx, databaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	logger.Info("database migrations applied")

	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	postgres.RegisterPoolMetrics(pool)

	if err := kafka.EnsureTopics(ctx, kafkaBroker); err != nil {
		logger.Warn("failed to ensure kafka topics", slog.String("error", err.Error()))
	}

	writers := map[string]*kafkago.Writer{
		kafka.TopicPaymentEvents: kafka.NewWriter([]string{kafkaBroker}, kafka.TopicPaymentEvents),
		kafka.TopicReviewEvents:  kafka.NewWriter([]string{kafkaBroker}, kafka.TopicReviewEvents),
	}
	for _, w := range writers {
		defer w.Close()
	}

	relay := infra.NewOutboxRelay(pool, writers, logger, 100*time.Millisecond, 50)
	go relay.Run(ctx)

	fraudConn, err := grpc.NewClient(fraudAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to fraud service: %w", err)
	}
	defer fraudConn.Close()

	fraudClient := infra.NewFraudClient(fraudv1.NewFraudServiceClient(fraudConn))

	var processor usecase.PaymentProcessor
	if stripeKey != "" {
		processor = infra.NewStripeProcessor(stripeKey)
		logger.Info("using stripe payment processor")
	} else {
		processor = infra.NewFakeProcessor(logger)
		logger.Info("using fake payment processor")
	}

	repo := infra.NewRepository(pool)
	authorizer := usecase.NewAuthorizer(repo, fraudClient, processor, logger)

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", grpcAddr, err)
	}

	server := grpc.NewServer(grpc.UnaryInterceptor(metrics.UnaryServerInterceptor()))
	ledgerv1.RegisterLedgerServiceServer(server, adapter.NewServer(authorizer))

	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()

	logger.Info("ledger listening", slog.String("addr", grpcAddr))
	return server.Serve(listener)
}
