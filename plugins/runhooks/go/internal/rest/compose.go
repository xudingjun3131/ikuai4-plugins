package rest

import (
	"context"
	"fmt"
)

// ComposeStep is one request in a compose sequence.
type ComposeStep struct {
	Name    string            `json:"name,omitempty"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// ComposeRequest runs steps sequentially (optionally repeating the whole sequence).
type ComposeRequest struct {
	Steps  []ComposeStep `json:"steps"`
	Repeat int           `json:"repeat,omitempty"` // times to run full sequence; default 1
}

// ComposeResult aggregates step results.
type ComposeResult struct {
	Results []SendResponse `json:"results"`
	Count   int            `json:"count"`
}

// Compose runs steps × repeat. Stops on context cancel; continues on HTTP errors.
func (s *Service) Compose(ctx context.Context, in ComposeRequest) (ComposeResult, error) {
	if len(in.Steps) == 0 {
		return ComposeResult{}, fmt.Errorf("steps required")
	}
	n := in.Repeat
	if n < 1 {
		n = 1
	}
	if n > 100 {
		n = 100
	}
	var out ComposeResult
	for r := 0; r < n; r++ {
		for _, step := range in.Steps {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			res, err := s.Send(ctx, SendRequest{
				Method:  step.Method,
				URL:     step.URL,
				Headers: step.Headers,
				Body:    step.Body,
			})
			if err != nil {
				res.Error = err.Error()
			}
			out.Results = append(out.Results, res)
			out.Count++
		}
	}
	return out, nil
}

// Repeat sends the same request n times.
func (s *Service) Repeat(ctx context.Context, in SendRequest, n int) (ComposeResult, error) {
	if n < 1 {
		n = 1
	}
	if n > 100 {
		n = 100
	}
	steps := make([]ComposeStep, n)
	for i := range steps {
		steps[i] = ComposeStep{
			Method:  in.Method,
			URL:     in.URL,
			Headers: in.Headers,
			Body:    in.Body,
		}
	}
	return s.Compose(ctx, ComposeRequest{Steps: steps, Repeat: 1})
}
