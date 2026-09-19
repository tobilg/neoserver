package datasource

import (
	"context"
	"errors"
	"testing"
)

type nilPreparationSource struct{ DataSource }

func (*nilPreparationSource) Close() error { panic("typed nil is not an opened handle") }
func (*nilPreparationSource) Health(context.Context) error {
	panic("typed nil is not an opened handle")
}

func TestPrepareRejectsNilResultsWithoutCallingHandleMethods(t *testing.T) {
	rejected := errors.New("connection rejected")
	for _, source := range []DataSource{nil, (*nilPreparationSource)(nil)} {
		got, err := Prepare(context.Background(), func() (DataSource, error) { return source, rejected })
		if got != nil || !errors.Is(err, rejected) {
			t.Fatalf("got=%v err=%v", got, err)
		}
		got, err = Prepare(context.Background(), func() (DataSource, error) { return source, nil })
		if got != nil || err == nil {
			t.Fatal("empty constructor reported success")
		}
	}
}
