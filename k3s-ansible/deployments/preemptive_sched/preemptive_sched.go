package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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

// RunningPod represents a running Pod's resource usage and priority.
type RunningPod struct {
	Name       string
	Namespace  string
	CPURequest float64
	MemRequest float64
	// (Additional resource usage fields could be added here.)
	Priority int
}

// getNodeStats gathers metrics from Prometheus for a given node.
// It queries for CPU and memory usage as before, and now also for disk and network.
// For disk and network, it uses two queries each: one for current usage (via rate())
// and one for the hardware capacity (assumed exposed by metrics).
func getNodeStats(nodeName string) (NodeStats, error) {
	var stats NodeStats
	var err error

	log.Printf("Getting stats for node: %s", nodeName)

	// ---------- CPU Metrics ----------
	cpuQuery := "sum(rate(node_cpu_seconds_total{mode!=\"idle\"}[5m])) by (instance)"
	cpuMetrics, err := queryPrometheus(cpuQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch CPU metrics: %v", err)
		stats.CPUTotal = 6.0
		stats.CPUFree = 2.0
	} else {
		var cpuUsage float64
		var nodeFound bool
		if usage, exists := cpuMetrics[nodeName+":9100"]; exists {
			cpuUsage = usage
			nodeFound = true
		} else {
			for instance, usage := range cpuMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					cpuUsage = usage
					nodeFound = true
					break
				}
			}
			if !nodeFound && len(cpuMetrics) > 0 {
				for _, usage := range cpuMetrics {
					cpuUsage = usage
					nodeFound = true
					break
				}
			}
		}
		if !nodeFound {
			stats.CPUTotal = 6.0
			stats.CPUFree = 2.0
		} else {
			stats.CPUTotal = 6.0
			stats.CPUFree = stats.CPUTotal - cpuUsage
		}
	}

	// ---------- Memory Metrics ----------
	memQuery := "node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes"
	memMetrics, err := queryPrometheus(memQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch Memory metrics: %v", err)
		stats.MemTotal = 8 * 1024 * 1024 * 1024 // 8GB
		stats.MemFree = 4 * 1024 * 1024 * 1024  // 4GB
	} else {
		var memUsed float64
		var nodeFound bool
		if usage, exists := memMetrics[nodeName+":9100"]; exists {
			memUsed = usage
			nodeFound = true
		} else {
			for instance, usage := range memMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					memUsed = usage
					nodeFound = true
					break
				}
			}
			if !nodeFound && len(memMetrics) > 0 {
				for _, usage := range memMetrics {
					memUsed = usage
					nodeFound = true
					break
				}
			}
		}
		if !nodeFound {
			stats.MemTotal = 8 * 1024 * 1024 * 1024
			stats.MemFree = 4 * 1024 * 1024 * 1024
		} else {
			memTotalQuery := "node_memory_MemTotal_bytes"
			memTotalMetrics, err := queryPrometheus(memTotalQuery)
			if err != nil {
				stats.MemTotal = 8 * 1024 * 1024 * 1024
			} else {
				if total, exists := memTotalMetrics[nodeName+":9100"]; exists {
					stats.MemTotal = total
				} else {
					for _, total := range memTotalMetrics {
						stats.MemTotal = total
						break
					}
				}
			}
			stats.MemFree = stats.MemTotal - memUsed
		}
	}

	// ---------- Disk Metrics ----------
	// Disk Read
	diskReadUsageQuery := "rate(node_disk_read_bytes_total[5m])"
	diskReadMetrics, err := queryPrometheus(diskReadUsageQuery)
	var diskReadUsage float64
	if err != nil {
		log.Printf("Warning: Failed to fetch disk read usage: %v", err)
		diskReadUsage = 0.0
	} else {
		var found bool
		if usage, exists := diskReadMetrics[nodeName+":9100"]; exists {
			diskReadUsage = usage
			found = true
		} else {
			for instance, usage := range diskReadMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					diskReadUsage = usage
					found = true
					break
				}
			}
			if !found && len(diskReadMetrics) > 0 {
				for _, usage := range diskReadMetrics {
					diskReadUsage = usage
					found = true
					break
				}
			}
		}
	}
	diskReadTotalQuery := "node_disk_read_bytes_max"
	diskReadTotalMetrics, err := queryPrometheus(diskReadTotalQuery)
	var diskReadTotal float64
	if err != nil {
		log.Printf("Warning: Failed to fetch disk read total capacity: %v", err)
		diskReadTotal = 100.0 // fallback value (e.g., 100 MB/s)
	} else {
		var found bool
		if total, exists := diskReadTotalMetrics[nodeName+":9100"]; exists {
			diskReadTotal = total
			found = true
		} else {
			for instance, total := range diskReadTotalMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					diskReadTotal = total
					found = true
					break
				}
			}
			if !found && len(diskReadTotalMetrics) > 0 {
				for _, total := range diskReadTotalMetrics {
					diskReadTotal = total
					found = true
					break
				}
			}
		}
	}
	stats.DiskReadTotal = diskReadTotal
	stats.DiskReadFree = diskReadTotal - diskReadUsage

	// Disk Write
	diskWriteUsageQuery := "rate(node_disk_written_bytes_total[5m])"
	diskWriteMetrics, err := queryPrometheus(diskWriteUsageQuery)
	var diskWriteUsage float64
	if err != nil {
		log.Printf("Warning: Failed to fetch disk write usage: %v", err)
		diskWriteUsage = 0.0
	} else {
		var found bool
		if usage, exists := diskWriteMetrics[nodeName+":9100"]; exists {
			diskWriteUsage = usage
			found = true
		} else {
			for instance, usage := range diskWriteMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					diskWriteUsage = usage
					found = true
					break
				}
			}
			if !found && len(diskWriteMetrics) > 0 {
				for _, usage := range diskWriteMetrics {
					diskWriteUsage = usage
					found = true
					break
				}
			}
		}
	}
	diskWriteTotalQuery := "node_disk_write_bytes_max"
	diskWriteTotalMetrics, err := queryPrometheus(diskWriteTotalQuery)
	var diskWriteTotal float64
	if err != nil {
		log.Printf("Warning: Failed to fetch disk write total capacity: %v", err)
		diskWriteTotal = 100.0 // fallback value
	} else {
		var found bool
		if total, exists := diskWriteTotalMetrics[nodeName+":9100"]; exists {
			diskWriteTotal = total
			found = true
		} else {
			for instance, total := range diskWriteTotalMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					diskWriteTotal = total
					found = true
					break
				}
			}
			if !found && len(diskWriteTotalMetrics) > 0 {
				for _, total := range diskWriteTotalMetrics {
					diskWriteTotal = total
					found = true
					break
				}
			}
		}
	}
	stats.DiskWriteTotal = diskWriteTotal
	stats.DiskWriteFree = diskWriteTotal - diskWriteUsage

	// ---------- Network Metrics ----------
	// Network Upload (Transmit)
	netUpUsageQuery := "rate(node_network_transmit_bytes_total[5m])"
	netUpMetrics, err := queryPrometheus(netUpUsageQuery)
	var netUpUsage float64
	if err != nil {
		log.Printf("Warning: Failed to fetch network upload usage: %v", err)
		netUpUsage = 0.0
	} else {
		var found bool
		if usage, exists := netUpMetrics[nodeName+":9100"]; exists {
			netUpUsage = usage
			found = true
		} else {
			for instance, usage := range netUpMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					netUpUsage = usage
					found = true
					break
				}
			}
			if !found && len(netUpMetrics) > 0 {
				for _, usage := range netUpMetrics {
					netUpUsage = usage
					found = true
					break
				}
			}
		}
	}
	// Network Download (Receive)
	netDownUsageQuery := "rate(node_network_receive_bytes_total[5m])"
	netDownMetrics, err := queryPrometheus(netDownUsageQuery)
	var netDownUsage float64
	if err != nil {
		log.Printf("Warning: Failed to fetch network download usage: %v", err)
		netDownUsage = 0.0
	} else {
		var found bool
		if usage, exists := netDownMetrics[nodeName+":9100"]; exists {
			netDownUsage = usage
			found = true
		} else {
			for instance, usage := range netDownMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					netDownUsage = usage
					found = true
					break
				}
			}
			if !found && len(netDownMetrics) > 0 {
				for _, usage := range netDownMetrics {
					netDownUsage = usage
					found = true
					break
				}
			}
		}
	}
	// Network Capacity (assume same capacity for upload and download)
	netSpeedQuery := "node_network_speed_bytes"
	netSpeedMetrics, err := queryPrometheus(netSpeedQuery)
	var netSpeed float64
	if err != nil {
		log.Printf("Warning: Failed to fetch network speed: %v", err)
		netSpeed = 1000.0 // fallback value, e.g., 1000 bytes/sec
	} else {
		var found bool
		if speed, exists := netSpeedMetrics[nodeName+":9100"]; exists {
			netSpeed = speed
			found = true
		} else {
			for instance, speed := range netSpeedMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					netSpeed = speed
					found = true
					break
				}
			}
			if !found && len(netSpeedMetrics) > 0 {
				for _, speed := range netSpeedMetrics {
					netSpeed = speed
					found = true
					break
				}
			}
		}
	}
	stats.NetUpTotal = netSpeed
	stats.NetUpFree = netSpeed - netUpUsage
	stats.NetDownTotal = netSpeed
	stats.NetDownFree = netSpeed - netDownUsage

	log.Printf("Node stats for %s: CPU Total: %.2f, CPU Free: %.2f, Mem Total: %.2f GB, Mem Free: %.2f GB",
		nodeName, stats.CPUTotal, stats.CPUFree, stats.MemTotal/(1024*1024*1024), stats.MemFree/(1024*1024*1024))
	log.Printf("Disk Read Total: %.2f, Free: %.2f; Disk Write Total: %.2f, Free: %.2f", stats.DiskReadTotal, stats.DiskReadFree, stats.DiskWriteTotal, stats.DiskWriteFree)
	log.Printf("Net Up Total: %.2f, Free: %.2f; Net Down Total: %.2f, Free: %.2f", stats.NetUpTotal, stats.NetUpFree, stats.NetDownTotal, stats.NetDownFree)
	return stats, nil
}

// queryPrometheus queries the Prometheus API and returns a map of instance strings to float64 values.
func queryPrometheus(query string) (map[string]float64, error) {
	prometheusURL := getPrometheusURL()
	args := url.Values{}
	args.Add("query", query)
	constructedURL := fmt.Sprintf("%s?%s", prometheusURL, args.Encode())

	log.Printf("Querying Prometheus: %s", constructedURL)
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", constructedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making GET request: %v", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %v", err)
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("query returned non-success status: %v", result.Status)
	}
	metrics := make(map[string]float64)
	for _, r := range result.Data.Result {
		key := r.Metric["instance"]
		if key == "" {
			key = fmt.Sprintf("metric_%d", len(metrics))
		}
		if len(r.Value) < 2 {
			continue
		}
		valueStr, ok := r.Value[1].(string)
		if !ok {
			continue
		}
		metrics[key] = parseFloat(valueStr)
	}
	return metrics, nil
}

// parseFloat converts a string to a float64.
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// getPrometheusURL returns the Prometheus URL from the environment or a default value.
func getPrometheusURL() string {
	baseUrl := os.Getenv("PROMETHEUS_URL")
	if baseUrl == "" {
		baseUrl = "http://prometheus-server.default.svc.cluster.local:80/api/v1/query"
	}
	return baseUrl
}

// getRunningPods lists all running pods on the given node.
func getRunningPods(client kubernetes.Interface, nodeName string) ([]RunningPod, error) {
	podList, err := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}
	var runningPods []RunningPod
	for _, pod := range podList.Items {
		if pod.Status.Phase != v1.PodRunning {
			continue
		}
		cpuReq, memReq := 0.0, 0.0
		for _, container := range pod.Spec.Containers {
			if req, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
				cpuReq += float64(req.MilliValue()) / 1000.0
			}
			if req, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
				memReq += float64(req.Value())
			}
		}
		prio := 0
		if pod.Spec.Priority != nil {
			prio = int(*pod.Spec.Priority)
		}
		runningPods = append(runningPods, RunningPod{
			Name:       pod.Name,
			Namespace:  pod.Namespace,
			CPURequest: cpuReq,
			MemRequest: memReq,
			Priority:   prio,
		})
	}
	return runningPods, nil
}

// evictPod evicts a pod using the Kubernetes eviction API.
func evictPod(client kubernetes.Interface, pod RunningPod) error {
	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}
	return client.PolicyV1().Evictions(eviction.Namespace).Evict(context.Background(), eviction)
}

// canScheduleMulti checks if a node has sufficient capacity across all resources.
func canScheduleMulti(pod PodRequest, stats NodeStats, alpha float64) bool {
	resources := []struct {
		free  float64
		total float64
		req   float64
		name  string
	}{
		{stats.CPUFree, stats.CPUTotal, pod.CPU, "cpu"},
		{stats.MemFree, stats.MemTotal, pod.Mem, "mem"},
		{stats.DiskReadFree, stats.DiskReadTotal, pod.DiskRead, "diskRead"},
		{stats.DiskWriteFree, stats.DiskWriteTotal, pod.DiskWrite, "diskWrite"},
		{stats.NetUpFree, stats.NetUpTotal, pod.NetUp, "netUp"},
		{stats.NetDownFree, stats.NetDownTotal, pod.NetDown, "netDown"},
	}

	for _, r := range resources {
		if r.req == 0 {
			continue
		}
		if r.free < r.req {
			log.Printf("Not enough %s: free=%v, req=%v", r.name, r.free, r.req)
			return false
		}
		expectedUtil := 1 - ((r.free - r.req) / r.total)
		if expectedUtil > alpha {
			log.Printf("%s utilization too high: expected %v > threshold %v", r.name, expectedUtil, alpha)
			return false
		}
	}
	return true
}

// scoreMultiResource computes a score based on the dominant resource share.
func scoreMultiResource(pod PodRequest, stats NodeStats, maxScore int) int {
	dominantShare := 0.0
	resources := []struct {
		free float64
		req  float64
		name string
	}{
		{stats.CPUFree, pod.CPU, "cpu"},
		{stats.MemFree, pod.Mem, "mem"},
		{stats.DiskReadFree, pod.DiskRead, "diskRead"},
		{stats.DiskWriteFree, pod.DiskWrite, "diskWrite"},
		{stats.NetUpFree, pod.NetUp, "netUp"},
		{stats.NetDownFree, pod.NetDown, "netDown"},
	}

	for _, r := range resources {
		if r.req == 0 {
			continue
		}
		share := r.req / r.free
		if share > dominantShare {
			dominantShare = share
		}
	}
	if dominantShare == 0 {
		return maxScore / 2
	}
	score := float64(maxScore) - (float64(maxScore) * dominantShare)
	if score < 0 {
		score = 0
	}
	return int(score)
}

// attemptPreemptionMulti evicts lower-priority pods to free enough resources.
func attemptPreemptionMulti(client kubernetes.Interface, pod PodRequest, nodeName string, stats NodeStats) bool {
	runningPods, err := getRunningPods(client, nodeName)
	if err != nil {
		log.Printf("Error getting running pods: %v", err)
		return false
	}

	var candidates []RunningPod
	for _, rp := range runningPods {
		if rp.Priority < pod.Priority {
			candidates = append(candidates, rp)
		}
	}
	if len(candidates) == 0 {
		log.Println("No candidate pods for preemption found.")
		return false
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	freedResources := struct {
		cpu       float64
		mem       float64
		diskRead  float64
		diskWrite float64
		netUp     float64
		netDown   float64
	}{}

	var victims []RunningPod
	for _, candidate := range candidates {
		freedResources.cpu += candidate.CPURequest
		freedResources.mem += candidate.MemRequest
		// Extend with additional resources if tracked.
		victims = append(victims, candidate)
		canScheduleAfterPreemption := (stats.CPUFree+freedResources.cpu >= pod.CPU) &&
			(stats.MemFree+freedResources.mem >= pod.Mem)
		// Add similar checks for disk and network as needed.
		if canScheduleAfterPreemption {
			for _, victim := range victims {
				if err := evictPod(client, victim); err != nil {
					log.Printf("Failed to evict pod %s/%s: %v", victim.Namespace, victim.Name, err)
					return false
				}
				log.Printf("Evicted pod %s/%s", victim.Namespace, victim.Name)
			}
			return true
		}
	}
	return false
}

func main() {
	// Command-line flags.
	nodeName := flag.String("node", "", "Name of the node to check (if empty, will use the first available node)")
	cpuReq := flag.Float64("cpu", 1.0, "CPU cores requested by the Pod")
	memReq := flag.Float64("mem", 1024*1024*1024, "Memory requested by the Pod in bytes")
	diskReadReq := flag.Float64("diskread", 0.0, "Disk read demand for the Pod (bytes/sec)")
	diskWriteReq := flag.Float64("diskwrite", 0.0, "Disk write demand for the Pod (bytes/sec)")
	netUpReq := flag.Float64("netup", 0.0, "Network upload demand for the Pod (bytes/sec)")
	netDownReq := flag.Float64("netdown", 0.0, "Network download demand for the Pod (bytes/sec)")
	podPriority := flag.Int("priority", 0, "Priority of the Pod")
	alpha := flag.Float64("alpha", 0.8, "Maximum resource utilization threshold (0.0-1.0)")
	preemption := flag.Bool("preemption", true, "Enable preemption of lower-priority Pods")
	interval := flag.Int("interval", 60, "Interval in seconds between checks")
	watchMode := flag.Bool("watch", true, "Enable watching for unscheduled pods")
	flag.Parse()

	// Create Kubernetes client.
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("Error creating Kubernetes client config: %v", err)
		}
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	// Start a goroutine to watch for unscheduled pods if watch mode is enabled
	if *watchMode {
		log.Println("Starting to watch for unscheduled pods...")
		go watchForUnscheduledPods(client, *alpha, *preemption)
	}

	for {
		targetNode := *nodeName
		if targetNode == "" {
			nodes, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			if err != nil {
				log.Printf("Error listing nodes: %v", err)
				time.Sleep(time.Duration(*interval) * time.Second)
				continue
			}
			if len(nodes.Items) == 0 {
				log.Printf("No nodes found in the cluster")
				time.Sleep(time.Duration(*interval) * time.Second)
				continue
			}
			targetNode = nodes.Items[0].Name
			var nodeIP string
			for _, addr := range nodes.Items[0].Status.Addresses {
				if addr.Type == v1.NodeInternalIP || addr.Type == v1.NodeExternalIP {
					nodeIP = addr.Address
					break
				}
			}
			if nodeIP != "" {
				log.Printf("No node specified, using node: %s (IP: %s)", targetNode, nodeIP)
				targetNode = nodeIP
			} else {
				log.Printf("No node specified, using node: %s", targetNode)
			}
		}

		// Test Prometheus connection.
		testQuery := "up"
		testMetrics, err := queryPrometheus(testQuery)
		if err != nil {
			log.Printf("Warning: Prometheus connection test failed: %v", err)
		} else {
			log.Printf("Prometheus connection test successful. Metrics: %+v", testMetrics)
		}

		stats, err := getNodeStats(targetNode)
		if err != nil {
			log.Printf("Error getting node stats: %v", err)
			time.Sleep(time.Duration(*interval) * time.Second)
			continue
		}

		// Build the Pod request.
		podReq := PodRequest{
			CPU:       *cpuReq,
			Mem:       *memReq,
			DiskRead:  *diskReadReq,
			DiskWrite: *diskWriteReq,
			NetUp:     *netUpReq,
			NetDown:   *netDownReq,
			Priority:  *podPriority,
		}

		if canScheduleMulti(podReq, stats, *alpha) {
			log.Printf("Node %s can schedule the Pod.", targetNode)
		} else {
			log.Printf("Node %s lacks sufficient resources.", targetNode)
			if *preemption {
				log.Printf("Preemption enabled; attempting to free resources on node %s.", targetNode)
				if attemptPreemptionMulti(client, podReq, targetNode, stats) {
					stats, err = getNodeStats(targetNode)
					if err != nil {
						log.Printf("Error re-fetching node stats: %v", err)
						time.Sleep(time.Duration(*interval) * time.Second)
						continue
					}
					if canScheduleMulti(podReq, stats, *alpha) {
						log.Printf("After preemption, node %s can now schedule the Pod.", targetNode)
					} else {
						log.Printf("Even after preemption, node %s still cannot schedule the Pod.", targetNode)
					}
				} else {
					log.Printf("Preemption failed on node %s.", targetNode)
				}
			} else {
				log.Printf("Preemption disabled; cannot schedule Pod on node %s.", targetNode)
			}
		}

		log.Printf("Sleeping for %d seconds before next check...", *interval)
		time.Sleep(time.Duration(*interval) * time.Second)
	}
}

// watchForUnscheduledPods watches for pods that have no node assigned and attempts to schedule them
func watchForUnscheduledPods(client kubernetes.Interface, alpha float64, enablePreemption bool) {
	for {
		// Get all pods in the cluster
		pods, err := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
			FieldSelector: "spec.schedulerName=preemptive-scheduler,spec.nodeName=",
		})
		if err != nil {
			log.Printf("Error listing pods: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Process each unscheduled pod
		for _, pod := range pods.Items {
			log.Printf("Found unscheduled pod: %s/%s", pod.Namespace, pod.Name)

			// Skip pods that are being deleted
			if pod.DeletionTimestamp != nil {
				log.Printf("Pod %s/%s is being deleted, skipping", pod.Namespace, pod.Name)
				continue
			}

			// Get pod resource requirements
			podReq := extractPodRequirements(&pod)

			// Find a suitable node for the pod
			nodeName, err := findNodeForPod(client, podReq, alpha, enablePreemption)
			if err != nil {
				log.Printf("Error finding node for pod %s/%s: %v", pod.Namespace, pod.Name, err)
				continue
			}

			if nodeName == "" {
				log.Printf("No suitable node found for pod %s/%s", pod.Namespace, pod.Name)
				continue
			}

			// Bind the pod to the node
			err = bindPodToNode(client, &pod, nodeName)
			if err != nil {
				log.Printf("Error binding pod %s/%s to node %s: %v", pod.Namespace, pod.Name, nodeName, err)
				continue
			}

			log.Printf("Successfully scheduled pod %s/%s on node %s", pod.Namespace, pod.Name, nodeName)
		}

		// Sleep before checking again
		time.Sleep(1 * time.Second)
	}
}

// extractPodRequirements extracts resource requirements from a pod
func extractPodRequirements(pod *v1.Pod) PodRequest {
	var cpuReq, memReq float64
	var priority int

	// Get CPU and memory requests from all containers
	for _, container := range pod.Spec.Containers {
		if container.Resources.Requests != nil {
			if cpu, ok := container.Resources.Requests[v1.ResourceCPU]; ok {
				cpuReq += float64(cpu.MilliValue()) / 1000.0
			}
			if mem, ok := container.Resources.Requests[v1.ResourceMemory]; ok {
				memReq += float64(mem.Value())
			}
		}
	}

	// Get pod priority
	if pod.Spec.Priority != nil {
		priority = int(*pod.Spec.Priority)
	}

	return PodRequest{
		CPU:      cpuReq,
		Mem:      memReq,
		Priority: priority,
		// Set other fields to 0 as they're not typically specified in pod specs
		DiskRead:  0,
		DiskWrite: 0,
		NetUp:     0,
		NetDown:   0,
	}
}

// findNodeForPod finds a suitable node for the pod
func findNodeForPod(client kubernetes.Interface, podReq PodRequest, alpha float64, enablePreemption bool) (string, error) {
	// Get all nodes
	nodes, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("error listing nodes: %v", err)
	}

	// Check each node
	for _, node := range nodes.Items {
		nodeName := node.Name

		// Skip nodes that are not ready or are unschedulable
		if !isNodeReady(&node) || node.Spec.Unschedulable {
			log.Printf("Node %s is not ready or is unschedulable, skipping", nodeName)
			continue
		}

		// Get node stats
		stats, err := getNodeStats(nodeName)
		if err != nil {
			log.Printf("Error getting stats for node %s: %v", nodeName, err)
			continue
		}

		// Check if the node can schedule the pod
		if canScheduleMulti(podReq, stats, alpha) {
			return nodeName, nil
		}

		// If preemption is enabled, try to free up resources
		if enablePreemption {
			log.Printf("Attempting preemption on node %s", nodeName)
			if attemptPreemptionMulti(client, podReq, nodeName, stats) {
				// Re-check if the node can now schedule the pod
				stats, err = getNodeStats(nodeName)
				if err != nil {
					log.Printf("Error re-fetching stats for node %s: %v", nodeName, err)
					continue
				}

				if canScheduleMulti(podReq, stats, alpha) {
					return nodeName, nil
				}
			}
		}
	}

	// No suitable node found
	return "", nil
}

// isNodeReady checks if a node is in Ready condition
func isNodeReady(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

// bindPodToNode binds a pod to a node
func bindPodToNode(client kubernetes.Interface, pod *v1.Pod, nodeName string) error {
	binding := &v1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Target: v1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       nodeName,
		},
	}

	return client.CoreV1().Pods(pod.Namespace).Bind(context.Background(), binding, metav1.CreateOptions{})
}
