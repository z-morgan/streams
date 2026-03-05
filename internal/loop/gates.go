package loop

import "github.com/zmorgan/streams/internal/stream"

// Gate evaluates a quality criterion against review output.
type Gate interface {
	Name() string
	Evaluate(reviewText string) stream.GateResult
}
