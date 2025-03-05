package multiresource

import (
	"fmt"
	"sync"
	"time"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// Name is the name of the plugin used in the plugin registry and configurations.
const Name = "MultiResource"

// Plugin is the main implementation that contains both filter and score functionality
type Plugin struct {
	handle     framework.Handle
	promClient *PrometheusClient
	alpha      float64
	maxScore   int

	// Cache for node stats
	mu        sync.RWMutex
	nodeStats map[string]NodeStats
}

// NodeStats holds statistics about node resource usage
type NodeStats struct {
	CPUTotal       float64 // Total CPU cores
	CPUFree        float64 // Free CPU cores
	MemTotal       float64 // Total memory in bytes
	MemFree        float64 // Free memory in bytes
	DiskReadTotal  float64 // Total disk read throughput capacity
	DiskReadFree   float64 // Available disk read throughput
	DiskWriteTotal float64 // Total disk write throughput capacity
	DiskWriteFree  float64 // Available disk write throughput
	NetUpTotal     float64 // Total network upload capacity
	NetUpFree      float64 // Available network upload capacity
	NetDownTotal   float64 // Total network download capacity
	NetDownFree    float64 // Available network download capacity
}

// PodRequest holds resource requirements for a pod
type PodRequest struct {
	CPU       float64 // CPU cores requested
	Mem       float64 // Memory requested in bytes
	DiskRead  float64 // Disk read demand (bytes/sec)
	DiskWrite float64 // Disk write demand (bytes/sec)
	NetUp     float64 // Network upload demand (bytes/sec)
	NetDown   float64 // Network download demand (bytes/sec)
	Priority  int     // Higher value means higher priority
}

// New initializes a new plugin and returns it.
func New(h framework.Handle) (framework.Plugin, error) {
	// Use default values
	alpha := 0.5
	maxScore := 100
	promURL := "http://prometheus-server.default.svc.cluster.local:80"

	klog.V(2).Infof("Creating MultiResource plugin with alpha=%v, maxScore=%v, prometheusURL=%v",
		alpha, maxScore, promURL)

	plugin := &Plugin{
		handle:     h,
		promClient: NewPrometheusClient(promURL),
		alpha:      alpha,
		maxScore:   maxScore,
		nodeStats:  make(map[string]NodeStats),
	}

	// Start the goroutine to update node stats periodically
	go plugin.startNodeStatsUpdater()

	return plugin, nil
}

// Name returns the name of the plugin
func (p *Plugin) Name() string {
	return Name
}

// startNodeStatsUpdater periodically updates the node stats
func (p *Plugin) startNodeStatsUpdater() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		p.updateAllNodeStats()
		<-ticker.C
	}
}

// updateAllNodeStats updates the stats for all nodes
func (p *Plugin) updateAllNodeStats() {
	nodes, err := p.handle.SnapshotSharedLister().NodeInfos().List()
	if err != nil {
		klog.Errorf("Error getting node list: %v", err)
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, nodeInfo := range nodes {
		nodeName := nodeInfo.Node().Name
		stats, err := p.getNodeStats(nodeName)
		if err != nil {
			klog.Errorf("Error getting stats for node %s: %v", nodeName, err)
			continue
		}
		p.nodeStats[nodeName] = stats
	}
}

// getNodeStatsFromCache returns node stats from the cache
func (p *Plugin) getNodeStatsFromCache(nodeName string) (NodeStats, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats, ok := p.nodeStats[nodeName]
	if !ok {
		return NodeStats{}, fmt.Errorf("no stats found for node %s", nodeName)
	}

	return stats, nil
}
