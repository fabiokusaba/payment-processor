package fsm_test

import (
	"context"
	"testing"

	"github.com/huncoding/payment-processor-course/pkg/fsm"
)

type payload struct {
	visited []string
	score float64
}

func record(p *payload, name string) {
	p.visited = append(p.visited, name)
}

func TestRunHappyPath(t *testing.T) {
	tests := []struct {
		name string
		wantTrace int
		wantScore float64
	}{
		{
			name: "happy path",
			wantTrace: 2,
			wantScore: 0.9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := payload{}
			m := fsm.New[payload]("start").Handle("start", func(ctx context.Context, data *payload) (fsm.State, error) {
				record(data, "start")
				return "score", nil
			}).Handle("score", func(ctx context.Context, data *payload) (fsm.State, error) {
				record(data, "score")
				data.score = 0.9
				return "done", nil
			}).Terminal("done")

			trace, err := m.Run(context.Background(), &p)
			if err != nil {
				t.Fatalf("unexpected error: %v\n", err)
			}

			if len(trace) != tt.wantTrace {
				t.Errorf("expected %d transitions, got %d\n", tt.wantTrace, len(trace))
			}

			if p.score != tt.wantScore {
				t.Errorf("expected score %.2f, got %.2f\n", tt.wantScore, p.score)
			}
		})
	}
}