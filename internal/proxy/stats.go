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
	DataPlaneStats   DataPlaneStats
	OutboundStats    OutboundStats
	IngressStats     IngressStats
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
		DataPlaneStats:   r.dataplane.Stats(),
		OutboundStats:    r.OutboundStats(),
		IngressStats:     r.ingressSnapshot(),
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
	fmt.Fprintf(&b, "dataplane_active_sessions\t%d\n", s.DataPlaneStats.ActiveSessions)
	fmt.Fprintf(&b, "dataplane_session_limit\t%d\n", s.DataPlaneStats.SessionLimit)
	fmt.Fprintf(&b, "dataplane_sessions_created\t%d\n", s.DataPlaneStats.SessionsCreated)
	fmt.Fprintf(&b, "dataplane_sessions_closed\t%d\n", s.DataPlaneStats.SessionsClosed)
	fmt.Fprintf(&b, "dataplane_packets_total\t%d\n", s.DataPlaneStats.PacketsTotal)
	fmt.Fprintf(&b, "dataplane_packets_encrypted\t%d\n", s.DataPlaneStats.PacketsEncrypted)
	fmt.Fprintf(&b, "dataplane_packets_handshake\t%d\n", s.DataPlaneStats.PacketsHandshake)
	fmt.Fprintf(&b, "dataplane_packets_dropped\t%d\n", s.DataPlaneStats.PacketsDropped)
	fmt.Fprintf(&b, "dataplane_packets_parse_errors\t%d\n", s.DataPlaneStats.PacketsParseErrors)
	fmt.Fprintf(&b, "dataplane_packets_route_errors\t%d\n", s.DataPlaneStats.PacketsRouteErrors)
	fmt.Fprintf(&b, "dataplane_packets_rejected_limit\t%d\n", s.DataPlaneStats.PacketsRejectedByLimit)
	fmt.Fprintf(&b, "dataplane_packets_rejected_dh_rate\t%d\n", s.DataPlaneStats.PacketsRejectedByDH)
	fmt.Fprintf(&b, "dataplane_packets_outbound_errors\t%d\n", s.DataPlaneStats.PacketsOutboundErrors)
	fmt.Fprintf(&b, "dataplane_bytes_total\t%d\n", s.DataPlaneStats.BytesTotal)
	fmt.Fprintf(&b, "outbound_dials\t%d\n", s.OutboundStats.Dials)
	fmt.Fprintf(&b, "outbound_dial_errors\t%d\n", s.OutboundStats.DialErrors)
	fmt.Fprintf(&b, "outbound_sends\t%d\n", s.OutboundStats.Sends)
	fmt.Fprintf(&b, "outbound_send_errors\t%d\n", s.OutboundStats.SendErrors)
	fmt.Fprintf(&b, "outbound_bytes_sent\t%d\n", s.OutboundStats.BytesSent)
	fmt.Fprintf(&b, "outbound_responses\t%d\n", s.OutboundStats.Responses)
	fmt.Fprintf(&b, "outbound_response_errors\t%d\n", s.OutboundStats.ResponseErrors)
	fmt.Fprintf(&b, "outbound_response_bytes\t%d\n", s.OutboundStats.ResponseBytes)
	fmt.Fprintf(&b, "outbound_active_sends\t%d\n", s.OutboundStats.ActiveSends)
	fmt.Fprintf(&b, "outbound_active_conns\t%d\n", s.OutboundStats.ActiveConns)
	fmt.Fprintf(&b, "outbound_pool_hits\t%d\n", s.OutboundStats.PoolHits)
	fmt.Fprintf(&b, "outbound_pool_misses\t%d\n", s.OutboundStats.PoolMisses)
	fmt.Fprintf(&b, "outbound_reconnects\t%d\n", s.OutboundStats.Reconnects)
	fmt.Fprintf(&b, "outbound_idle_evictions\t%d\n", s.OutboundStats.IdleEvictions)
	fmt.Fprintf(&b, "outbound_closed_after_send\t%d\n", s.OutboundStats.ClosedAfterSend)
	fmt.Fprintf(&b, "ingress_active_connections\t%d\n", s.IngressStats.ActiveConnections)
	fmt.Fprintf(&b, "ingress_accepted_connections\t%d\n", s.IngressStats.AcceptedConnections)
	fmt.Fprintf(&b, "ingress_accept_rate_limited\t%d\n", s.IngressStats.AcceptRateLimited)
	fmt.Fprintf(&b, "ingress_closed_connections\t%d\n", s.IngressStats.ClosedConnections)
	fmt.Fprintf(&b, "ingress_frames_received\t%d\n", s.IngressStats.FramesReceived)
	fmt.Fprintf(&b, "ingress_frames_handled\t%d\n", s.IngressStats.FramesHandled)
	fmt.Fprintf(&b, "ingress_frames_returned\t%d\n", s.IngressStats.FramesReturned)
	fmt.Fprintf(&b, "ingress_frames_failed\t%d\n", s.IngressStats.FramesFailed)
	fmt.Fprintf(&b, "ingress_bytes_received\t%d\n", s.IngressStats.BytesReceived)
	fmt.Fprintf(&b, "ingress_bytes_returned\t%d\n", s.IngressStats.BytesReturned)
	fmt.Fprintf(&b, "ingress_read_errors\t%d\n", s.IngressStats.ReadErrors)
	fmt.Fprintf(&b, "ingress_write_errors\t%d\n", s.IngressStats.WriteErrors)
	fmt.Fprintf(&b, "ingress_invalid_frames\t%d\n", s.IngressStats.InvalidFrames)
	return b.String()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
