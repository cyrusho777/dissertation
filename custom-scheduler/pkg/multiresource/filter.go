package multiresource

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// Filter checks if a pod can fit on a node based on its resource requirements
func (p *Plugin) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	nodeName := nodeInfo.Node().Name

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

			// If we still can't get node stats, reject the node
			return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("Failed to get node %s stats", nodeName))
		}

		// Cache the updated stats
		p.mu.Lock()
		p.nodeStats[nodeName] = nodeStats
		p.mu.Unlock()
	}

	// Check if the pod fits on this node
	if !p.canScheduleMulti(podReq, nodeStats) {
		reason := fmt.Sprintf("Node %s doesn't have enough resources for pod %s/%s",
			nodeName, pod.Namespace, pod.Name)
		klog.V(3).Infof(reason)
		return framework.NewStatus(framework.Unschedulable, reason)
	}

	return nil
}

// canScheduleMulti checks if a pod can be scheduled on a node based on
// CPU, memory, disk I/O, and network bandwidth requirements
func (p *Plugin) canScheduleMulti(podReq PodRequest, nodeStats NodeStats) bool {
	// Check CPU
	if podReq.CPU > nodeStats.CPUFree {
		klog.V(4).Infof("Not enough CPU. Requested: %v, Available: %v",
			podReq.CPU, nodeStats.CPUFree)
		return false
	}

	// Check Memory
	if podReq.Mem > nodeStats.MemFree {
		klog.V(4).Infof("Not enough Memory. Requested: %v, Available: %v",
			podReq.Mem, nodeStats.MemFree)
		return false
	}

	// Check Disk Read
	if podReq.DiskRead > nodeStats.DiskReadFree {
		klog.V(4).Infof("Not enough Disk Read. Requested: %v, Available: %v",
			podReq.DiskRead, nodeStats.DiskReadFree)
		return false
	}

	// Check Disk Write
	if podReq.DiskWrite > nodeStats.DiskWriteFree {
		klog.V(4).Infof("Not enough Disk Write. Requested: %v, Available: %v",
			podReq.DiskWrite, nodeStats.DiskWriteFree)
		return false
	}

	// Check Network Upload
	if podReq.NetUp > nodeStats.NetUpFree {
		klog.V(4).Infof("Not enough Network Upload. Requested: %v, Available: %v",
			podReq.NetUp, nodeStats.NetUpFree)
		return false
	}

	// Check Network Download
	if podReq.NetDown > nodeStats.NetDownFree {
		klog.V(4).Infof("Not enough Network Download. Requested: %v, Available: %v",
			podReq.NetDown, nodeStats.NetDownFree)
		return false
	}

	klog.V(4).Infof("Pod can be scheduled. Pod requirements: %+v, Node stats: %+v",
		podReq, nodeStats)
	return true
}

// extractPodRequirements extracts resource requirements from a pod
func (p *Plugin) extractPodRequirements(pod *v1.Pod) PodRequest {
	var req PodRequest

	// Set default priority
	req.Priority = 1

	// Extract CPU and memory from container resource requirements
	for _, container := range pod.Spec.Containers {
		// CPU
		cpuReq := container.Resources.Requests.Cpu()
		if !cpuReq.IsZero() {
			req.CPU += float64(cpuReq.MilliValue()) / 1000.0
		}

		// Memory
		memReq := container.Resources.Requests.Memory()
		if !memReq.IsZero() {
			req.Mem += float64(memReq.Value())
		}
	}

	// Extract disk I/O and network bandwidth from annotations
	req.DiskRead = parseResourceAnnotation(pod, "scheduler.extender/disk-read", req.CPU*10*1024*1024)
	req.DiskWrite = parseResourceAnnotation(pod, "scheduler.extender/disk-write", req.CPU*5*1024*1024)
	req.NetUp = parseResourceAnnotation(pod, "scheduler.extender/net-up", req.CPU*5*1024*1024)
	req.NetDown = parseResourceAnnotation(pod, "scheduler.extender/net-down", req.CPU*10*1024*1024)

	klog.V(4).Infof("Extracted requirements for pod %s/%s: %+v", pod.Namespace, pod.Name, req)
	return req
}

// parseResourceAnnotation parses resource values from pod annotations with support for units
func parseResourceAnnotation(pod *v1.Pod, key string, defaultValue float64) float64 {
	if pod.Annotations == nil {
		return defaultValue
	}

	if value, exists := pod.Annotations[key]; exists {
		// Try to parse the value as a plain number
		if parsedValue, err := strconv.ParseFloat(value, 64); err == nil {
			return parsedValue
		}

		// Try parsing with units (K, M, G)
		if len(value) > 1 {
			unit := strings.ToUpper(value[len(value)-1:])
			numPart := value[:len(value)-1]

			if parsedNum, err := strconv.ParseFloat(numPart, 64); err == nil {
				switch unit {
				case "K":
					return parsedNum * 1024
				case "M":
					return parsedNum * 1024 * 1024
				case "G":
					return parsedNum * 1024 * 1024 * 1024
				}
			}
		}
	}

	return defaultValue
}
