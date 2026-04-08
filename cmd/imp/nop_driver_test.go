package imp

import (
	"context"
	"errors"
	"testing"

	"github.com/seoyhaein/spawner/pkg/api"
)

func TestNopDriver_AlwaysReturnsK8sUnavailable(t *testing.T) {
	d := NopDriver{}

	if _, err := d.Prepare(context.Background(), api.RunSpec{}); !errors.Is(err, ErrK8sUnavailable) {
		t.Fatalf("Prepare error = %v", err)
	}
	if _, err := d.Start(context.Background(), testPrepared{}); !errors.Is(err, ErrK8sUnavailable) {
		t.Fatalf("Start error = %v", err)
	}
	if _, err := d.Wait(context.Background(), testHandle{}); !errors.Is(err, ErrK8sUnavailable) {
		t.Fatalf("Wait error = %v", err)
	}
	if err := d.Signal(context.Background(), testHandle{}, api.Signal{}); !errors.Is(err, ErrK8sUnavailable) {
		t.Fatalf("Signal error = %v", err)
	}
	if err := d.Cancel(context.Background(), testHandle{}); !errors.Is(err, ErrK8sUnavailable) {
		t.Fatalf("Cancel error = %v", err)
	}
}
