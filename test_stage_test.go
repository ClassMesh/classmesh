package classmesh

import "context"

type staticStage struct {
	name       string
	categories map[string]string
}

func newStatic(name string, categories map[string]string) *staticStage {
	return &staticStage{name: name, categories: categories}
}

func (s *staticStage) Name() string {
	return s.name
}

func (s *staticStage) Classify(ctx context.Context, record Record) (Classification, error) {
	if err := ctx.Err(); err != nil {
		return Classification{}, err
	}
	category, ok := s.categories[string(record.Data)]
	if !ok {
		return Classification{}, ErrUnclassified
	}
	return Classification{Category: category, Confidence: 1, Stage: s.name}, nil
}
