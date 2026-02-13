package proxy

import (
	"fmt"
	"strings"
	"time"

	"github.com/TelegramMessenger/MTProxy/internal/config"
)

type RuntimeStats struct {
	GeneratedAt time.Time

	ConfigLoadedAt   time.Time
	ConfigSize       int
	ConfigMD5        string
	ConfigClusters   int
	ConfigFilename   string
	WarningsCount    int
	ForwardStats     ForwardStats
	RouterStats      RouterStats
	ManagerStats     config.ManagerStats
	HealthyTargets   int
	UnhealthyTargets int
	HasCurrentConfig bool
}

func (r *Runtime) StatsSnapshot() RuntimeStats {
	now := time.Now().UTC()
	snapshot, warnings, ok := r.lifecycle.Current()
	out := RuntimeStats{
		GeneratedAt:      now,
		WarningsCount:    len(warnings),
		ForwardStats:     r.forwarder.Stats(),
		RouterStats:      r.router.Stats(),
		ManagerStats:     r.lifecycle.ManagerStats(),
		HasCurrentConfig: ok,
	}
	out.HealthyTargets, out.UnhealthyTargets = r.TargetHealthStats()
	if !ok {
		return out
	}

	out.ConfigLoadedAt = snapshot.LoadedAt
	out.ConfigSize = snapshot.Bytes
	out.ConfigMD5 = snapshot.MD5Hex
	out.ConfigClusters = len(snapshot.Config.Clusters)
	out.ConfigFilename = snapshot.SourcePath
	return out
}

func (s RuntimeStats) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "stats_generated_at\t%d\n", s.GeneratedAt.Unix())
	fmt.Fprintf(&b, "has_current_config\t%d\n", boolToInt(s.HasCurrentConfig))
	if s.HasCurrentConfig {
		fmt.Fprintf(&b, "config_filename\t%s\n", s.ConfigFilename)
		fmt.Fprintf(&b, "config_loaded_at\t%d\n", s.ConfigLoadedAt.Unix())
		fmt.Fprintf(&b, "config_size\t%d\n", s.ConfigSize)
		fmt.Fprintf(&b, "config_md5\t%s\n", s.ConfigMD5)
		fmt.Fprintf(&b, "config_auth_clusters\t%d\n", s.ConfigClusters)
	}
	fmt.Fprintf(&b, "router_default_cluster\t%d\n", s.RouterStats.DefaultClusterID)
	fmt.Fprintf(&b, "router_clusters\t%d\n", s.RouterStats.Clusters)
	fmt.Fprintf(&b, "router_targets\t%d\n", s.RouterStats.Targets)
	fmt.Fprintf(&b, "targets_healthy\t%d\n", s.HealthyTargets)
	fmt.Fprintf(&b, "targets_unhealthy\t%d\n", s.UnhealthyTargets)
	fmt.Fprintf(&b, "bootstrap_warnings\t%d\n", s.WarningsCount)
	fmt.Fprintf(&b, "config_check_calls\t%d\n", s.ManagerStats.CheckCalls)
	fmt.Fprintf(&b, "config_reload_calls\t%d\n", s.ManagerStats.ReloadCalls)
	fmt.Fprintf(&b, "config_reload_success\t%d\n", s.ManagerStats.ReloadSuccess)
	fmt.Fprintf(&b, "config_reload_last_error\t%s\n", s.ManagerStats.LastError)
	fmt.Fprintf(&b, "forward_total\t%d\n", s.ForwardStats.TotalRequests)
	fmt.Fprintf(&b, "forward_successful\t%d\n", s.ForwardStats.Successful)
	fmt.Fprintf(&b, "forward_failed\t%d\n", s.ForwardStats.Failed)
	fmt.Fprintf(&b, "forward_used_default\t%d\n", s.ForwardStats.UsedDefault)
	fmt.Fprintf(&b, "forward_bytes\t%d\n", s.ForwardStats.ForwardedBytes)
	fmt.Fprintf(&b, "forward_avg_payload_bytes\t%.3f\n", s.ForwardStats.AvgPayloadBytes)
	fmt.Fprintf(&b, "forward_last_error\t%s\n", s.ForwardStats.LastError)
	return b.String()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
