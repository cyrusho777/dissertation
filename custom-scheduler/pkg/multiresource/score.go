package multiresource

import (
	"context"
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// Score scores nodes based on resource availability
func (p *Plugin) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	// Extract pod requirements
	podReq := p.extractPodRequirements(pod)

	// Get node stats from cache
	nodeStats, err := p.getNodeStatsFromCache(nodeName)
	if err != nil {
		klog.Warningf("Error getting node stats for %s: %v", nodeName, err)

		// Attempt to refresh the stats for this node
		klog.Infof("Refreshing node stats for %s", nodeName)
		nodeStats, err = p.getNodeStats(nodeName)
		if err != nil {
			klog.Warningf("Still error getting node stats for %s: %v", nodeName, err)
			return 0, framework.NewStatus(framework.Error, fmt.Sprintf("Failed to get node %s stats", nodeName))
		}

		// Cache the updated stats
		p.mu.Lock()
		p.nodeStats[nodeName] = nodeStats
		p.mu.Unlock()
	}

	// Calculate score
	score := p.scoreMultiResource(podReq, nodeStats)
	klog.V(4).Infof("Score for node %s: %d", nodeName, score)

	return int64(score), nil
}

// ScoreExtensions returns an interface that provides the framework ScoreExtensions interface
func (p *Plugin) ScoreExtensions() framework.ScoreExtensions {
	return p
}

// NormalizeScore normalizes the scores across all nodes
func (p *Plugin) NormalizeScore(ctx context.Context, state *framework.CycleState, pod *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	// Find the highest and lowest scores
	var highest int64 = 0
	var lowest int64 = int64(p.maxScore)

	for _, nodeScore := range scores {
		if nodeScore.Score > highest {
			highest = nodeScore.Score
		}
		if nodeScore.Score < lowest {
			lowest = nodeScore.Score
		}
	}

	// If all scores are the same, return early
	if highest == lowest {
		return nil
	}

	// Normalize the scores to the range [0, maxScore]
	for i := range scores {
		if highest != lowest {
			scores[i].Score = ((scores[i].Score - lowest) * int64(p.maxScore)) / (highest - lowest)
		} else {
			scores[i].Score = int64(p.maxScore / 2)
		}
	}

	return nil
}

// scoreMultiResource calculates a score for a node based on its resource availability and the pod's requirements
// It considers CPU, memory, disk I/O, and network bandwidth
// Higher scores are better
func (p *Plugin) scoreMultiResource(podReq PodRequest, nodeStats NodeStats) int {
	// Temporary variables to calculate resource usage after the pod is placed
	cpuUsage := (nodeStats.CPUTotal - nodeStats.CPUFree + podReq.CPU) / nodeStats.CPUTotal
	memUsage := (nodeStats.MemTotal - nodeStats.MemFree + podReq.Mem) / nodeStats.MemTotal
	diskReadUsage := 0.0
	if nodeStats.DiskReadTotal > 0 {
		diskReadUsage = (nodeStats.DiskReadTotal - nodeStats.DiskReadFree + podReq.DiskRead) / nodeStats.DiskReadTotal
	}
	diskWriteUsage := 0.0
	if nodeStats.DiskWriteTotal > 0 {
		diskWriteUsage = (nodeStats.DiskWriteTotal - nodeStats.DiskWriteFree + podReq.DiskWrite) / nodeStats.DiskWriteTotal
	}
	netUpUsage := 0.0
	if nodeStats.NetUpTotal > 0 {
		netUpUsage = (nodeStats.NetUpTotal - nodeStats.NetUpFree + podReq.NetUp) / nodeStats.NetUpTotal
	}
	netDownUsage := 0.0
	if nodeStats.NetDownTotal > 0 {
		netDownUsage = (nodeStats.NetDownTotal - nodeStats.NetDownFree + podReq.NetDown) / nodeStats.NetDownTotal
	}

	// Ensure all usage values are within [0, 1]
	cpuUsage = math.Max(0.0, math.Min(1.0, cpuUsage))
	memUsage = math.Max(0.0, math.Min(1.0, memUsage))
	diskReadUsage = math.Max(0.0, math.Min(1.0, diskReadUsage))
	diskWriteUsage = math.Max(0.0, math.Min(1.0, diskWriteUsage))
	netUpUsage = math.Max(0.0, math.Min(1.0, netUpUsage))
	netDownUsage = math.Max(0.0, math.Min(1.0, netDownUsage))

	// Calculate balanced resource usage score
	// - For alpha=0: prefer spreading (lower usage is better)
	// - For alpha=1: prefer packing (higher usage is better)
	// We weight CPU and memory higher than I/O and network
	cpuScore := p.resourceToScore(cpuUsage, 0.4)
	memScore := p.resourceToScore(memUsage, 0.3)
	diskReadScore := p.resourceToScore(diskReadUsage, 0.075)
	diskWriteScore := p.resourceToScore(diskWriteUsage, 0.075)
	netUpScore := p.resourceToScore(netUpUsage, 0.075)
	netDownScore := p.resourceToScore(netDownUsage, 0.075)

	// Combine scores with weights
	totalScore := cpuScore + memScore + diskReadScore + diskWriteScore + netUpScore + netDownScore

	// Scale to maxScore
	finalScore := int(totalScore * float64(p.maxScore))

	if finalScore > p.maxScore {
		finalScore = p.maxScore
	}

	klog.V(5).Infof("Scores for node: CPU=%.2f, Mem=%.2f, DiskRead=%.2f, DiskWrite=%.2f, NetUp=%.2f, NetDown=%.2f, Total=%d",
		cpuScore, memScore, diskReadScore, diskWriteScore, netUpScore, netDownScore, finalScore)

	return finalScore
}

// resourceToScore converts a resource usage value to a score based on alpha
func (p *Plugin) resourceToScore(usage float64, weight float64) float64 {
	// For alpha=0, lower usage is better (spreading)
	// For alpha=1, higher usage is better (packing)
	var score float64
	if p.alpha < 0.5 {
		// Spreading: score = 1 - usage (adjusted by alpha)
		spreadingScore := 1.0 - usage
		packingScore := usage
		score = spreadingScore*(1-p.alpha*2) + packingScore*(p.alpha*2)
	} else {
		// Packing: score = usage (adjusted by alpha)
		spreadingScore := 1.0 - usage
		packingScore := usage
		score = spreadingScore*(2.0-p.alpha*2) + packingScore*((p.alpha-0.5)*2)
	}

	return score * weight
}
