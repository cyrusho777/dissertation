package sched_extension

import (
	"log"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
)

// NodeStats holds resource metrics for a node.
type NodeStats struct {
	CPUTotal       float64 // Total CPU cores.
	CPUFree        float64 // Free CPU cores.
	MemTotal       float64 // Total memory in bytes.
	MemFree        float64 // Free memory in bytes.
	DiskReadTotal  float64 // Total disk read throughput capacity.
	DiskReadFree   float64 // Available disk read throughput.
	DiskWriteTotal float64 // Total disk write throughput capacity.
	DiskWriteFree  float64 // Available disk write throughput.
	NetUpTotal     float64 // Total network upload capacity.
	NetUpFree      float64 // Available network upload capacity.
	NetDownTotal   float64 // Total network download capacity.
	NetDownFree    float64 // Available network download capacity.
}

// PodRequest represents a Pod's resource demands.
type PodRequest struct {
	CPU       float64 // CPU cores requested.
	Mem       float64 // Memory requested in bytes.
	DiskRead  float64 // Disk read demand (bytes/sec).
	DiskWrite float64 // Disk write demand (bytes/sec).
	NetUp     float64 // Network upload demand (bytes/sec).
	NetDown   float64 // Network download demand (bytes/sec).
	Priority  int     // Higher value means higher priority.
}

// parseResourceAnnotation parses a resource value from annotations with fallback
func parseResourceAnnotation(pod *v1.Pod, key string, defaultValue float64) float64 {
	if pod.Annotations == nil {
		return defaultValue
	}

	if value, exists := pod.Annotations[key]; exists {
		// Try to parse the value
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

// estimateIORequirements estimates I/O requirements based on pod properties
func estimateIORequirements(pod *v1.Pod) (diskRead, diskWrite, netUp, netDown float64) {
	// Default values
	diskRead = 10 * 1024 * 1024 // 10 MB/s
	diskWrite = 5 * 1024 * 1024 // 5 MB/s
	netUp = 1 * 1024 * 1024     // 1 MB/s
	netDown = 2 * 1024 * 1024   // 2 MB/s

	// Check annotations first
	diskRead = parseResourceAnnotation(pod, "scheduler.extender/disk-read", diskRead)
	diskWrite = parseResourceAnnotation(pod, "scheduler.extender/disk-write", diskWrite)
	netUp = parseResourceAnnotation(pod, "scheduler.extender/net-up", netUp)
	netDown = parseResourceAnnotation(pod, "scheduler.extender/net-down", netDown)

	// Try to make smarter estimates based on pod characteristics
	// For database workloads, increase disk I/O
	for _, container := range pod.Spec.Containers {
		// Check image name for common patterns
		imageLower := strings.ToLower(container.Image)

		// Database containers typically have higher I/O
		if strings.Contains(imageLower, "mysql") ||
			strings.Contains(imageLower, "postgres") ||
			strings.Contains(imageLower, "mongo") ||
			strings.Contains(imageLower, "redis") {
			diskRead = max(diskRead, 50*1024*1024)   // 50 MB/s
			diskWrite = max(diskWrite, 20*1024*1024) // 20 MB/s
		}

		// API/web servers typically have higher network I/O
		if strings.Contains(imageLower, "nginx") ||
			strings.Contains(imageLower, "apache") ||
			strings.Contains(imageLower, "node") ||
			strings.Contains(imageLower, "tomcat") {
			netUp = max(netUp, 20*1024*1024)     // 20 MB/s
			netDown = max(netDown, 40*1024*1024) // 40 MB/s
		}
	}

	// Scale I/O requirements with CPU and memory requests
	// Pods with higher CPU/memory likely need more I/O
	var totalCPU float64
	var totalMem float64

	for _, container := range pod.Spec.Containers {
		if container.Resources.Requests != nil {
			if cpu := container.Resources.Requests.Cpu(); cpu != nil {
				totalCPU += float64(cpu.MilliValue()) / 1000.0
			}
			if mem := container.Resources.Requests.Memory(); mem != nil {
				totalMem += float64(mem.Value())
			}
		}
	}

	// If the pod requests a lot of CPU, it might need more disk I/O
	if totalCPU >= 4.0 { // 4+ cores
		diskRead = max(diskRead, 100*1024*1024)  // 100 MB/s
		diskWrite = max(diskWrite, 50*1024*1024) // 50 MB/s
	} else if totalCPU >= 2.0 { // 2-4 cores
		diskRead = max(diskRead, 50*1024*1024)   // 50 MB/s
		diskWrite = max(diskWrite, 25*1024*1024) // 25 MB/s
	}

	// If the pod requests a lot of memory, it might need more network I/O
	if totalMem >= 4*1024*1024*1024 { // 4+ GB
		netUp = max(netUp, 50*1024*1024)      // 50 MB/s
		netDown = max(netDown, 100*1024*1024) // 100 MB/s
	} else if totalMem >= 1*1024*1024*1024 { // 1-4 GB
		netUp = max(netUp, 20*1024*1024)     // 20 MB/s
		netDown = max(netDown, 40*1024*1024) // 40 MB/s
	}

	return diskRead, diskWrite, netUp, netDown
}

// ExtractPodRequirements extracts resource requirements from a Pod
func ExtractPodRequirements(pod *v1.Pod) PodRequest {
	// Get pod priority
	priority := 0
	if pod.Spec.Priority != nil {
		priority = int(*pod.Spec.Priority)
	}

	// Calculate total CPU and memory requests
	var cpu float64
	var mem float64

	for _, container := range pod.Spec.Containers {
		if container.Resources.Requests != nil {
			if cpuRequest := container.Resources.Requests.Cpu(); cpuRequest != nil {
				cpu += float64(cpuRequest.MilliValue()) / 1000.0
			}
			if memRequest := container.Resources.Requests.Memory(); memRequest != nil {
				mem += float64(memRequest.Value())
			}
		}
	}

	// Estimate disk and network requirements
	diskRead, diskWrite, netUp, netDown := estimateIORequirements(pod)

	// Log the requirements for debugging
	log.Printf("Pod %s/%s requirements: CPU=%.2f cores, Mem=%.2f GB, DiskRead=%.2f MB/s, DiskWrite=%.2f MB/s, NetUp=%.2f MB/s, NetDown=%.2f MB/s",
		pod.Namespace,
		pod.Name,
		cpu,
		mem/(1024*1024*1024),
		diskRead/(1024*1024),
		diskWrite/(1024*1024),
		netUp/(1024*1024),
		netDown/(1024*1024))

	return PodRequest{
		CPU:       cpu,
		Mem:       mem,
		DiskRead:  diskRead,
		DiskWrite: diskWrite,
		NetUp:     netUp,
		NetDown:   netDown,
		Priority:  priority,
	}
}

// CanScheduleMulti checks if a Pod can be scheduled on a node based on multi-resource availability
func CanScheduleMulti(podReq PodRequest, stats NodeStats, alpha float64) bool {
	// Simplified scheduling decision
	if podReq.CPU > stats.CPUFree || podReq.Mem > stats.MemFree {
		return false
	}
	if podReq.DiskRead > stats.DiskReadFree || podReq.DiskWrite > stats.DiskWriteFree {
		return false
	}
	if podReq.NetUp > stats.NetUpFree || podReq.NetDown > stats.NetDownFree {
		return false
	}
	return true
}

// ScoreMultiResource computes a score based on the dominant resource share.
func ScoreMultiResource(podReq PodRequest, stats NodeStats, maxScore int) int {
	dominantShare := 0.0
	resources := []struct {
		free float64
		req  float64
		name string
	}{
		{stats.CPUFree, podReq.CPU, "cpu"},
		{stats.MemFree, podReq.Mem, "mem"},
		{stats.DiskReadFree, podReq.DiskRead, "diskRead"},
		{stats.DiskWriteFree, podReq.DiskWrite, "diskWrite"},
		{stats.NetUpFree, podReq.NetUp, "netUp"},
		{stats.NetDownFree, podReq.NetDown, "netDown"},
	}
	for _, r := range resources {
		if r.req == 0 {
			continue
		}
		share := r.req / r.free
		if share > dominantShare {
			dominantShare = share
			log.Printf("New dominant resource: %s with share %.4f", r.name, share)
		}
	}
	score := float64(maxScore) - (float64(maxScore) * dominantShare)
	if score < 0 {
		score = 0
	}
	return int(score)
}

// Helper function for taking maximum of two float64 values
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
