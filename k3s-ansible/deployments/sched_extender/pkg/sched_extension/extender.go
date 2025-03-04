package sched_extension

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	schedulerapi "k8s.io/kube-scheduler/extender/v1"
)

// MultiResourceExtender implements the scheduler extender interface
type MultiResourceExtender struct {
	alpha    float64
	maxScore int
	// A thread-safe cache of NodeStats keyed by node name.
	cache sync.RWMutex
	// Map from node name to NodeStats.
	nodeStats map[string]NodeStats
}

// NewMultiResourceExtender creates a new scheduler extender for multi-resource scheduling
func NewMultiResourceExtender() *MultiResourceExtender {
	// Read alpha and maxScore from environment variables.
	alpha := 0.8
	if val := os.Getenv("MULTIRESOURCE_ALPHA"); val != "" {
		if a, err := strconv.ParseFloat(val, 64); err == nil {
			alpha = a
		}
	}
	maxScore := 100
	if val := os.Getenv("MULTIRESOURCE_MAXSCORE"); val != "" {
		if ms, err := strconv.Atoi(val); err == nil {
			maxScore = ms
		}
	}

	ext := &MultiResourceExtender{
		alpha:     alpha,
		maxScore:  maxScore,
		nodeStats: make(map[string]NodeStats),
	}

	// Start background updater to refresh NodeStats cache.
	go ext.startNodeStatsUpdater()

	return ext
}

// Filter filters out nodes that cannot run the pod
func (e *MultiResourceExtender) Filter(args schedulerapi.ExtenderArgs) *schedulerapi.ExtenderFilterResult {
	pod := args.Pod
	nodes := args.Nodes
	var filteredNodes []v1.Node
	failedNodes := make(schedulerapi.FailedNodesMap)

	log.Printf("Filter called for pod: %s/%s", pod.Namespace, pod.Name)

	for _, node := range nodes.Items {
		stats, err := e.getNodeStatsFromCache(node.Name)
		if err != nil {
			failedNodes[node.Name] = fmt.Sprintf("error retrieving stats: %v", err)
			continue
		}

		podReq := ExtractPodRequirements(pod)
		if !CanScheduleMulti(podReq, stats, e.alpha) {
			failedNodes[node.Name] = "insufficient resources"
			continue
		}

		filteredNodes = append(filteredNodes, node)
	}

	return &schedulerapi.ExtenderFilterResult{
		Nodes: &v1.NodeList{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NodeList",
				APIVersion: "v1",
			},
			Items: filteredNodes,
		},
		FailedNodes: failedNodes,
		Error:       "",
	}
}

// Prioritize assigns scores to nodes based on their resource availability
func (e *MultiResourceExtender) Prioritize(args schedulerapi.ExtenderArgs) *schedulerapi.HostPriorityList {
	pod := args.Pod
	nodes := args.Nodes

	log.Printf("Prioritize called for pod: %s/%s", pod.Namespace, pod.Name)

	priorityList := make(schedulerapi.HostPriorityList, 0, len(nodes.Items))
	podReq := ExtractPodRequirements(pod)

	for _, node := range nodes.Items {
		stats, err := e.getNodeStatsFromCache(node.Name)
		if err != nil {
			log.Printf("Warning: error retrieving stats for node %s: %v", node.Name, err)
			priorityList = append(priorityList, schedulerapi.HostPriority{
				Host:  node.Name,
				Score: 0,
			})
			continue
		}

		score := ScoreMultiResource(podReq, stats, e.maxScore)
		priorityList = append(priorityList, schedulerapi.HostPriority{
			Host:  node.Name,
			Score: int64(score),
		})
	}

	return &priorityList
}

// startNodeStatsUpdater periodically updates the NodeStats cache for all nodes.
func (e *MultiResourceExtender) startNodeStatsUpdater() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Initial update
	e.updateAllNodeStats()

	for {
		<-ticker.C
		e.updateAllNodeStats()
	}
}

// updateAllNodeStats updates stats for all nodes
func (e *MultiResourceExtender) updateAllNodeStats() {
	// Here we would normally query the Kubernetes API to get all nodes
	// For simplicity, we'll use the Prometheus query to discover nodes
	cpuQuery := "sum(rate(node_cpu_seconds_total{mode!=\"idle\"}[5m])) by (instance)"
	cpuMetrics, err := QueryPrometheus(cpuQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch CPU metrics: %v", err)
		return
	}

	// Extract node names from the metrics
	for instance := range cpuMetrics {
		nodeName := strings.Split(instance, ":")[0]
		stats, err := GetNodeStats(nodeName)
		if err != nil {
			log.Printf("Updater: error getting stats for node %s: %v", nodeName, err)
			continue
		}
		e.cache.Lock()
		e.nodeStats[nodeName] = stats
		e.cache.Unlock()
	}
}

// getNodeStatsFromCache retrieves NodeStats for a node from the local cache.
func (e *MultiResourceExtender) getNodeStatsFromCache(nodeName string) (NodeStats, error) {
	e.cache.RLock()
	stats, exists := e.nodeStats[nodeName]
	e.cache.RUnlock()
	if !exists {
		return NodeStats{}, fmt.Errorf("node stats not available for %s", nodeName)
	}
	return stats, nil
}

// SetupHTTPServer configures and returns an HTTP server for the scheduler extender
func SetupHTTPServer(port int) *http.Server {
	extender := NewMultiResourceExtender()

	mux := http.NewServeMux()
	mux.HandleFunc("/filter", func(w http.ResponseWriter, r *http.Request) {
		HandleExtenderRequest(w, r, extender.Filter)
	})

	mux.HandleFunc("/prioritize", func(w http.ResponseWriter, r *http.Request) {
		HandleExtenderRequest(w, r, extender.Prioritize)
	})

	// Add a health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return server
}
