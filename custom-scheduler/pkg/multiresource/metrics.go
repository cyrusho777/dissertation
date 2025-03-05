package multiresource

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
)

// PrometheusClient handles communication with the Prometheus API
type PrometheusClient struct {
	api v1.API
	url string
}

// NewPrometheusClient creates a new PrometheusClient
func NewPrometheusClient(url string) *PrometheusClient {
	// If URL is provided via environment variable, use that instead
	envURL := os.Getenv("PROMETHEUS_URL")
	if envURL != "" {
		url = envURL
	}

	klog.Infof("Creating Prometheus client with base URL: %s", url)

	// Remove any API path that might be included in the URL
	if strings.Contains(url, "/api/v1/query") {
		url = strings.Split(url, "/api/v1/query")[0]
		klog.Infof("Removed API path from URL, using base URL: %s", url)
	}

	client, err := api.NewClient(api.Config{
		Address: url,
	})
	if err != nil {
		klog.Errorf("Error creating Prometheus client: %v", err)
		return &PrometheusClient{url: url}
	}

	return &PrometheusClient{
		api: v1.NewAPI(client),
		url: url,
	}
}

// Query executes a Prometheus query and returns the results as a map of node names to values
func (c *PrometheusClient) Query(query string) (map[string]float64, error) {
	if c.api == nil {
		return nil, fmt.Errorf("prometheus API client not initialized")
	}

	// Create a curl-equivalent command for debugging
	curlCmd := fmt.Sprintf("curl -s \"%s/api/v1/query?query=%s\"", c.url, url.QueryEscape(query))
	klog.Infof("Equivalent curl command: %s", curlCmd)

	// Debug log to print the query and URL
	klog.Infof("Executing Prometheus query: %s using client with URL: %s", query, c.url)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, warnings, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		klog.Errorf("Error querying Prometheus with query '%s' at URL '%s': %v", query, c.url, err)
		return nil, err
	}

	if len(warnings) > 0 {
		klog.Warningf("Warnings from Prometheus query: %v", warnings)
	}

	resultMap := make(map[string]float64)

	switch resultType := result.(type) {
	case model.Vector:
		vector := result.(model.Vector)
		klog.Infof("Query '%s' returned %d samples", query, len(vector))
		for _, sample := range vector {
			nodeName := string(sample.Metric["instance"])
			// Remove port number if present
			if idx := lastIndex(nodeName, ":"); idx != -1 {
				nodeName = nodeName[:idx]
			}

			// If node label exists, use that instead
			if node, ok := sample.Metric["node"]; ok {
				nodeName = string(node)
			}

			resultMap[nodeName] = float64(sample.Value)
		}
	default:
		return nil, fmt.Errorf("unsupported result type: %T", resultType)
	}

	klog.Infof("Query '%s' result map: %+v", query, resultMap)
	return resultMap, nil
}

// lastIndex returns the index of the last instance of sep in s, or -1 if sep is not present in s.
func lastIndex(s, sep string) int {
	for i := len(s) - len(sep); i >= 0; i-- {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

// getNodeStats retrieves all metrics for a node from Prometheus
func (p *Plugin) getNodeStats(nodeName string) (NodeStats, error) {
	var stats NodeStats
	var err error

	klog.Infof("Getting node stats for node: %s", nodeName)
	if p.promClient == nil {
		klog.Errorf("Prometheus client is nil for node %s", nodeName)
		return stats, fmt.Errorf("prometheus client not initialized")
	}

	// Get CPU metrics
	cpuQuery := fmt.Sprintf(`count(node_cpu_seconds_total{mode="idle",instance=~"%s.*"})`, nodeName)
	klog.Infof("Executing CPU metrics query for node %s: %s", nodeName, cpuQuery)
	cpuResult, err := p.promClient.Query(cpuQuery)
	if err != nil {
		klog.Errorf("Error querying CPU metrics for node %s: %v", nodeName, err)
	} else if value, ok := cpuResult[nodeName]; ok {
		stats.CPUTotal = value
		klog.Infof("Found CPU total for node %s: %v", nodeName, value)
	} else {
		klog.Warningf("No CPU metrics found for node %s", nodeName)
		stats.CPUTotal = 1.0 // Default to 1 core if not found
	}

	// Get CPU usage (non-idle)
	cpuUsageQuery := fmt.Sprintf(`1 - avg(rate(node_cpu_seconds_total{mode="idle",instance=~"%s.*"}[5m]))`, nodeName)
	klog.Infof("Executing CPU usage query for node %s: %s", nodeName, cpuUsageQuery)
	cpuUsageResult, err := p.promClient.Query(cpuUsageQuery)
	if err != nil {
		klog.Errorf("Error querying CPU usage metrics for node %s: %v", nodeName, err)
		stats.CPUFree = stats.CPUTotal * 0.2 // Default to 20% free if error
	} else if value, ok := cpuUsageResult[nodeName]; ok {
		usageRatio := value
		if usageRatio > 1.0 {
			usageRatio = 1.0
		}
		stats.CPUFree = stats.CPUTotal * (1.0 - usageRatio)
		klog.Infof("Found CPU usage for node %s: %v, calculated free: %v", nodeName, usageRatio, stats.CPUFree)
	} else {
		klog.Warningf("No CPU usage metrics found for node %s", nodeName)
		stats.CPUFree = stats.CPUTotal * 0.2 // Default to 20% free if not found
	}

	// Get memory metrics
	memTotalQuery := fmt.Sprintf(`node_memory_MemTotal_bytes{instance=~"%s.*"}`, nodeName)
	memTotalResult, err := p.promClient.Query(memTotalQuery)
	if err != nil {
		klog.Errorf("Error querying memory total metrics: %v", err)
	} else if value, ok := memTotalResult[nodeName]; ok {
		stats.MemTotal = value
	} else {
		klog.Warningf("No memory total metrics found for node %s", nodeName)
		stats.MemTotal = 4 * 1024 * 1024 * 1024 // Default to 4GB if not found
	}

	memAvailableQuery := fmt.Sprintf(`node_memory_MemAvailable_bytes{instance=~"%s.*"}`, nodeName)
	memAvailableResult, err := p.promClient.Query(memAvailableQuery)
	if err != nil {
		klog.Errorf("Error querying memory available metrics: %v", err)
		stats.MemFree = stats.MemTotal * 0.2 // Default to 20% free if error
	} else if value, ok := memAvailableResult[nodeName]; ok {
		stats.MemFree = value
	} else {
		klog.Warningf("No memory available metrics found for node %s", nodeName)
		stats.MemFree = stats.MemTotal * 0.2 // Default to 20% free if not found
	}

	// Get disk I/O metrics
	// For disk read throughput - use a 5-minute rate
	diskReadQuery := fmt.Sprintf(`sum(rate(node_disk_read_bytes_total{instance=~"%s.*"}[5m]))`, nodeName)
	diskReadResult, err := p.promClient.Query(diskReadQuery)
	if err != nil {
		klog.Errorf("Error querying disk read metrics: %v", err)
	} else if value, ok := diskReadResult[nodeName]; ok {
		// Current read rate, we'll consider this as 80% of capacity
		stats.DiskReadTotal = value / 0.8
		stats.DiskReadFree = stats.DiskReadTotal - value
	} else {
		klog.Warningf("No disk read metrics found for node %s", nodeName)
		stats.DiskReadTotal = 100 * 1024 * 1024        // Default to 100MB/s if not found
		stats.DiskReadFree = stats.DiskReadTotal * 0.2 // Default to 20% free
	}

	// For disk write throughput
	diskWriteQuery := fmt.Sprintf(`sum(rate(node_disk_written_bytes_total{instance=~"%s.*"}[5m]))`, nodeName)
	diskWriteResult, err := p.promClient.Query(diskWriteQuery)
	if err != nil {
		klog.Errorf("Error querying disk write metrics: %v", err)
	} else if value, ok := diskWriteResult[nodeName]; ok {
		// Current write rate, we'll consider this as 80% of capacity
		stats.DiskWriteTotal = value / 0.8
		stats.DiskWriteFree = stats.DiskWriteTotal - value
	} else {
		klog.Warningf("No disk write metrics found for node %s", nodeName)
		stats.DiskWriteTotal = 50 * 1024 * 1024          // Default to 50MB/s if not found
		stats.DiskWriteFree = stats.DiskWriteTotal * 0.2 // Default to 20% free
	}

	// Get network metrics
	// For network upload throughput
	netUpQuery := fmt.Sprintf(`sum(rate(node_network_transmit_bytes_total{instance=~"%s.*"}[5m]))`, nodeName)
	netUpResult, err := p.promClient.Query(netUpQuery)
	if err != nil {
		klog.Errorf("Error querying network upload metrics: %v", err)
	} else if value, ok := netUpResult[nodeName]; ok {
		// Current upload rate, we'll consider this as 80% of capacity
		stats.NetUpTotal = value / 0.8
		stats.NetUpFree = stats.NetUpTotal - value
	} else {
		klog.Warningf("No network upload metrics found for node %s", nodeName)
		stats.NetUpTotal = 125 * 1024 * 1024     // Default to 1Gbps if not found
		stats.NetUpFree = stats.NetUpTotal * 0.2 // Default to 20% free
	}

	// For network download throughput
	netDownQuery := fmt.Sprintf(`sum(rate(node_network_receive_bytes_total{instance=~"%s.*"}[5m]))`, nodeName)
	netDownResult, err := p.promClient.Query(netDownQuery)
	if err != nil {
		klog.Errorf("Error querying network download metrics: %v", err)
	} else if value, ok := netDownResult[nodeName]; ok {
		// Current download rate, we'll consider this as 80% of capacity
		stats.NetDownTotal = value / 0.8
		stats.NetDownFree = stats.NetDownTotal - value
	} else {
		klog.Warningf("No network download metrics found for node %s", nodeName)
		stats.NetDownTotal = 125 * 1024 * 1024       // Default to 1Gbps if not found
		stats.NetDownFree = stats.NetDownTotal * 0.2 // Default to 20% free
	}

	klog.V(4).Infof("Node stats for %s: %+v", nodeName, stats)
	return stats, nil
}
