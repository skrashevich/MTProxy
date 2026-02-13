package proxy

import (
	"errors"
	"testing"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type mockChooser struct {
	result ChooseResult
	err    error
}

func (m mockChooser) ChooseProxyTargetDetailed(targetDC int) (ChooseResult, error) {
	if m.err != nil {
		return ChooseResult{}, m.err
	}
	out := m.result
	out.RequestedCluster = targetDC
	return out, nil
}

func TestForwarderDecideSuccess(t *testing.T) {
	f := NewForwarder(mockChooser{
		result: ChooseResult{
			Target:            config.Target{ClusterID: 2, Host: "a", Port: 443},
			ResolvedClusterID: 2,
			UsedDefault:       true,
		},
	})

	d, err := f.Decide(ForwardRequest{TargetDC: 99, PayloadSize: 42})
	if err != nil {
		t.Fatalf("decide failed: %v", err)
	}
	if d.Target.Host != "a" || !d.UsedDefault {
		t.Fatalf("unexpected decision: %+v", d)
	}

	s := f.Stats()
	if s.TotalRequests != 1 || s.Successful != 1 || s.Failed != 0 || s.UsedDefault != 1 {
		t.Fatalf("unexpected stats: %+v", s)
	}
	if s.ForwardedBytes != 42 {
		t.Fatalf("unexpected forwarded bytes: %d", s.ForwardedBytes)
	}
	if s.AvgPayloadBytes != 42 {
		t.Fatalf("unexpected avg payload bytes: %f", s.AvgPayloadBytes)
	}
}

func TestForwarderDecideFailure(t *testing.T) {
	f := NewForwarder(mockChooser{
		err: errors.New("cluster unavailable"),
	})

	_, err := f.Decide(ForwardRequest{TargetDC: 4})
	if err == nil {
		t.Fatalf("expected decision error")
	}

	s := f.Stats()
	if s.TotalRequests != 1 || s.Successful != 0 || s.Failed != 1 {
		t.Fatalf("unexpected stats: %+v", s)
	}
	if s.LastError == "" {
		t.Fatalf("expected last error to be set")
	}
}
