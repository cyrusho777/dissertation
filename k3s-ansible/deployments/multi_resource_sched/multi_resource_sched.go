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
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
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
			log.Printf("Warning: Node %s not found in CPU metrics, using default values", nodeName)
			stats.CPUTotal = 6.0
			stats.CPUFree = 2.0
		} else {
			// Assuming 8 cores total for the node (adjust as needed).
			stats.CPUTotal = 6.0
			stats.CPUFree = stats.CPUTotal - cpuUsage
			if stats.CPUFree < 0 {
				stats.CPUFree = 0
			}
		}
	}

	// ---------- Memory Metrics ----------
	memTotalQuery := "node_memory_MemTotal_bytes"
	memFreeQuery := "node_memory_MemAvailable_bytes"
	memTotalMetrics, err := queryPrometheus(memTotalQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch memory total metrics: %v", err)
		stats.MemTotal = 32 * 1024 * 1024 * 1024 // 32 GB default.
	} else {
		var memTotal float64
		var nodeFound bool
		if total, exists := memTotalMetrics[nodeName+":9100"]; exists {
			memTotal = total
			nodeFound = true
		} else {
			for instance, total := range memTotalMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					memTotal = total
					nodeFound = true
					break
				}
			}
			if !nodeFound && len(memTotalMetrics) > 0 {
				for _, total := range memTotalMetrics {
					memTotal = total
					nodeFound = true
					break
				}
			}
		}
		if !nodeFound {
			log.Printf("Warning: Node %s not found in memory total metrics, using default values", nodeName)
			stats.MemTotal = 32 * 1024 * 1024 * 1024 // 32 GB default.
		} else {
			stats.MemTotal = memTotal
		}
	}

	memFreeMetrics, err := queryPrometheus(memFreeQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch memory free metrics: %v", err)
		stats.MemFree = 16 * 1024 * 1024 * 1024 // 16 GB default.
	} else {
		var memFree float64
		var nodeFound bool
		if free, exists := memFreeMetrics[nodeName+":9100"]; exists {
			memFree = free
			nodeFound = true
		} else {
			for instance, free := range memFreeMetrics {
				if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
					memFree = free
					nodeFound = true
					break
				}
			}
			if !nodeFound && len(memFreeMetrics) > 0 {
				for _, free := range memFreeMetrics {
					memFree = free
					nodeFound = true
					break
				}
			}
		}
		if !nodeFound {
			log.Printf("Warning: Node %s not found in memory free metrics, using default values", nodeName)
			stats.MemFree = 16 * 1024 * 1024 * 1024 // 16 GB default.
		} else {
			stats.MemFree = memFree
		}
	}

	// ---------- Disk Metrics ----------
	// For simplicity, we'll use default values for disk and network.
	// In a real implementation, you'd query Prometheus for these metrics.
	stats.DiskReadTotal = 100 * 1024 * 1024 // 100 MB/s read capacity.
	stats.DiskReadFree = 80 * 1024 * 1024   // 80 MB/s available read.
	stats.DiskWriteTotal = 50 * 1024 * 1024 // 50 MB/s write capacity.
	stats.DiskWriteFree = 40 * 1024 * 1024  // 40 MB/s available write.
	stats.NetUpTotal = 1000 * 1024 * 1024   // 1000 MB/s upload capacity.
	stats.NetUpFree = 800 * 1024 * 1024     // 800 MB/s available upload.
	stats.NetDownTotal = 1000 * 1024 * 1024 // 1000 MB/s download capacity.
	stats.NetDownFree = 800 * 1024 * 1024   // 800 MB/s available download.

	// Log the stats for debugging.
	log.Printf("Node stats for %s: CPU Total: %.2f, CPU Free: %.2f, Mem Total: %.2f GB, Mem Free: %.2f GB",
		nodeName, stats.CPUTotal, stats.CPUFree, stats.MemTotal/(1024*1024*1024), stats.MemFree/(1024*1024*1024))

	return stats, nil
}

// queryPrometheus sends a query to Prometheus and returns the results.
func queryPrometheus(query string) (map[string]float64, error) {
	baseUrl := getPrometheusURL()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", baseUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	q := url.Values{}
	q.Add("query", query)
	req.URL.RawQuery = q.Encode()
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
	score := float64(maxScore) - (float64(maxScore) * dominantShare)
	if score < 0 {
		score = 0
	}
	return int(score)
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
		go watchForUnscheduledPods(client, *alpha)
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
			log.Printf("Multi-resource scheduler does not support preemption; cannot schedule Pod on node %s.", targetNode)
		}

		log.Printf("Sleeping for %d seconds before next check...", *interval)
		time.Sleep(time.Duration(*interval) * time.Second)
	}
}

// watchForUnscheduledPods watches for pods that have no node assigned and attempts to schedule them
func watchForUnscheduledPods(client kubernetes.Interface, alpha float64) {
	for {
		// Get all pods in the cluster
		pods, err := client.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
			FieldSelector: "spec.schedulerName=multi-resource-scheduler,spec.nodeName=",
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
			nodeName, err := findNodeForPod(client, podReq, alpha)
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
func findNodeForPod(client kubernetes.Interface, podReq PodRequest, alpha float64) (string, error) {
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
