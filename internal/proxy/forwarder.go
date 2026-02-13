package proxy

import (
	"sync"
	"sync/atomic"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type ForwardRequest struct {
	TargetDC    int
	AuthKeyID   int64
	PayloadSize int
}

type ForwardDecision struct {
	Target            config.Target
	RequestedCluster  int
	ResolvedClusterID int
	UsedDefault       bool
}

type ForwardStats struct {
	TotalRequests   uint64
	Successful      uint64
	Failed          uint64
	UsedDefault     uint64
	ForwardedBytes  uint64
	AvgPayloadBytes float64
	LastError       string
}

type targetChooser interface {
	ChooseProxyTargetDetailed(targetDC int) (ChooseResult, error)
}

type Forwarder struct {
	chooser targetChooser

	totalRequests  atomic.Uint64
	successful     atomic.Uint64
	failed         atomic.Uint64
	usedDefault    atomic.Uint64
	forwardedBytes atomic.Uint64

	lastErrMu sync.RWMutex
	lastErr   string
}

func NewForwarder(chooser targetChooser) *Forwarder {
	return &Forwarder{
		chooser: chooser,
	}
}

func (f *Forwarder) Decide(req ForwardRequest) (ForwardDecision, error) {
	f.totalRequests.Add(1)

	result, err := f.chooser.ChooseProxyTargetDetailed(req.TargetDC)
	if err != nil {
		f.failed.Add(1)
		f.setLastError(err.Error())
		return ForwardDecision{}, err
	}

	f.successful.Add(1)
	if req.PayloadSize > 0 {
		f.forwardedBytes.Add(uint64(req.PayloadSize))
	}
	if result.UsedDefault {
		f.usedDefault.Add(1)
	}
	return ForwardDecision{
		Target:            result.Target,
		RequestedCluster:  result.RequestedCluster,
		ResolvedClusterID: result.ResolvedClusterID,
		UsedDefault:       result.UsedDefault,
	}, nil
}

func (f *Forwarder) Stats() ForwardStats {
	successful := f.successful.Load()
	forwardedBytes := f.forwardedBytes.Load()
	var avgPayloadBytes float64
	if successful > 0 {
		avgPayloadBytes = float64(forwardedBytes) / float64(successful)
	}
	return ForwardStats{
		TotalRequests:   f.totalRequests.Load(),
		Successful:      successful,
		Failed:          f.failed.Load(),
		UsedDefault:     f.usedDefault.Load(),
		ForwardedBytes:  forwardedBytes,
		AvgPayloadBytes: avgPayloadBytes,
		LastError:       f.getLastError(),
	}
}

func (f *Forwarder) setLastError(s string) {
	f.lastErrMu.Lock()
	defer f.lastErrMu.Unlock()
	f.lastErr = s
}

func (f *Forwarder) getLastError() string {
	f.lastErrMu.RLock()
	defer f.lastErrMu.RUnlock()
	return f.lastErr
}
