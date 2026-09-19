package datasource

import (
	"context"
	"errors"
	"reflect"
	"time"
)

// Constructors include legacy native calls without cancellation support. Bound
// their concurrency and the caller's wait; abandoned results are always closed.
var constructors = make(chan struct{}, 16)

func Prepare(ctx context.Context, construct func() (DataSource, error)) (DataSource, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case constructors <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	type result struct {
		source DataSource
		err    error
	}
	ready := make(chan result)
	go func() {
		defer func() { <-constructors }()
		source, err := construct()
		// Factory adapters commonly return a typed nil (*PostGIS, error) as
		// DataSource. It is not a handle and must never receive Health/Close.
		if source != nil {
			value := reflect.ValueOf(source)
			if value.Kind() == reflect.Pointer && value.IsNil() {
				source = nil
			}
		}
		if err == nil && source == nil {
			err = errors.New("datasource constructor returned no source")
		}
		if err == nil && source != nil {
			err = source.Health(ctx)
		}
		if err != nil && source != nil {
			_ = source.Close()
			source = nil
		}
		select {
		case ready <- result{source, err}:
		case <-ctx.Done():
			if source != nil {
				_ = source.Close()
			}
		}
	}()
	select {
	case item := <-ready:
		return item.source, item.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
