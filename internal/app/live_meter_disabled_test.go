package app

import (
	"context"
	"testing"

	"github.com/licoy/quosparr/internal/meter"
)

func TestDisabledMeterSkipsFetchAndDetect(t *testing.T) {
	fetches, detects := 0, 0
	off := true
	m := newMeterModule(enLocalizer(),
		func(context.Context) meter.State { fetches++; return meter.State{Status: meter.StatusOK} },
		func(context.Context) bool { detects++; return true })
	m.disabled = func() (bool, error) { return off, nil }

	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetches != 0 || m.raw.Status != meter.StatusNotConfigured {
		t.Fatalf("disabled refresh: fetches %d, status %v", fetches, m.raw.Status)
	}
	m.raw = meter.State{Status: meter.StatusOK}
	if err := m.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if detects != 0 || m.raw.Status != meter.StatusNotConfigured {
		t.Fatalf("disabled local: detects %d, status %v", detects, m.raw.Status)
	}

	off = false
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetches != 1 || m.raw.Status != meter.StatusOK {
		t.Fatalf("enabled refresh: fetches %d, status %v", fetches, m.raw.Status)
	}
}
