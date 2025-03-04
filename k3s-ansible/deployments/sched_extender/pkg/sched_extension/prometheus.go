package sched_extension

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// queryPrometheus sends a query to the Prometheus server and returns the result.
func QueryPrometheus(query string) (map[string]float64, error) {
	prometheusURL := os.Getenv("PROMETHEUS_URL")
	if prometheusURL == "" {
		prometheusURL = "http://prometheus-server.default.svc.cluster.local:80"
	}

	// Fix: Remove the "/api/v1/query" from the PROMETHEUS_URL if it's already included
	prometheusBaseURL := strings.TrimSuffix(prometheusURL, "/api/v1/query")
	queryURL := fmt.Sprintf("%s/api/v1/query?query=%s", prometheusBaseURL, url.QueryEscape(query))
	log.Printf("Querying Prometheus URL: %s", queryURL)

	resp, err := http.Get(queryURL)
	if err != nil {
		return nil, fmt.Errorf("error querying Prometheus: %v", err)
	}
	defer resp.Body.Close()

	// Check status code first
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading Prometheus response: %v", err)
	}

	log.Printf("Response body length: %d bytes", len(body))

	// Log a snippet of the response for debugging
	bodyPreview := string(body)
	if len(bodyPreview) > 100 {
		bodyPreview = bodyPreview[:100] + "..."
	}
	log.Printf("Response body preview: %s", bodyPreview)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error unmarshaling Prometheus response: %v", err)
	}

	// Parse the response
	metrics := make(map[string]float64)
	status, ok := result["status"].(string)
	if !ok || status != "success" {
		return nil, fmt.Errorf("Prometheus query failed: %s", string(body))
	}

	data, ok := result["data"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no data in Prometheus response")
	}

	resultType, ok := data["resultType"].(string)
	if !ok || resultType != "vector" {
		return nil, fmt.Errorf("unexpected result type: %s", resultType)
	}

	results, ok := data["result"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no results in Prometheus response")
	}

	for _, r := range results {
		result, ok := r.(map[string]interface{})
		if !ok {
			continue
		}

		metric, ok := result["metric"].(map[string]interface{})
		if !ok {
			continue
		}

		instance, ok := metric["instance"].(string)
		if !ok {
			continue
		}

		value, ok := result["value"].([]interface{})
		if !ok || len(value) != 2 {
			continue
		}

		floatValue, ok := value[1].(string)
		if !ok {
			continue
		}

		var v float64
		if _, err := fmt.Sscanf(floatValue, "%f", &v); err != nil {
			log.Printf("Warning: Could not parse value %s: %v", floatValue, err)
			continue
		}

		metrics[instance] = v
	}

	return metrics, nil
}

// getNodeStats gathers metrics from Prometheus for a given node.
func GetNodeStats(nodeName string) (NodeStats, error) {
	var stats NodeStats
	var err error

	log.Printf("Fetching stats for node: %s", nodeName)

	// ---------- CPU Metrics ----------
	cpuQuery := "sum(rate(node_cpu_seconds_total{mode!=\"idle\"}[5m])) by (instance)"
	cpuMetrics, err := QueryPrometheus(cpuQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch CPU metrics: %v", err)
		stats.CPUTotal = 6.0
		stats.CPUFree = 2.0
	} else {
		var cpuUsage float64
		var nodeFound bool
		instanceKey := nodeName + ":9100"
		if usage, exists := cpuMetrics[instanceKey]; exists {
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
	memTotalMetrics, err := QueryPrometheus(memTotalQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch memory total metrics: %v", err)
		stats.MemTotal = 32 * 1024 * 1024 * 1024
	} else {
		var memTotal float64
		var nodeFound bool
		instanceKey := nodeName + ":9100"
		if total, exists := memTotalMetrics[instanceKey]; exists {
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
			log.Printf("Warning: Node %s not found in memory metrics, using default values", nodeName)
			stats.MemTotal = 32 * 1024 * 1024 * 1024
		} else {
			stats.MemTotal = memTotal
		}
	}

	memFreeMetrics, err := QueryPrometheus(memFreeQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch memory free metrics: %v", err)
		stats.MemFree = 16 * 1024 * 1024 * 1024
	} else {
		var memFree float64
		var nodeFound bool
		instanceKey := nodeName + ":9100"
		if free, exists := memFreeMetrics[instanceKey]; exists {
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
			log.Printf("Warning: Node %s not found in memory metrics, using default values", nodeName)
			stats.MemFree = 16 * 1024 * 1024 * 1024
		} else {
			stats.MemFree = memFree
		}
	}

	// ---------- Disk I/O Metrics ----------
	// Query disk read rate (bytes per second)
	diskReadQuery := "rate(node_disk_read_bytes_total[5m])"
	diskReadMetrics, err := QueryPrometheus(diskReadQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch disk read metrics: %v", err)
		stats.DiskReadTotal = 500 * 1024 * 1024 // 500 MB/s default
		stats.DiskReadFree = 300 * 1024 * 1024  // 300 MB/s default
	} else {
		diskReadTotal := 0.0

		// Sum up disk read rates across all disks for this node
		for instance, readRate := range diskReadMetrics {
			if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
				diskReadTotal += readRate
			}
		}

		if diskReadTotal == 0 && len(diskReadMetrics) > 0 {
			// If node not found but metrics exist, use first available
			for _, readRate := range diskReadMetrics {
				diskReadTotal = readRate
				break
			}
		}

		if diskReadTotal == 0 {
			log.Printf("Warning: No disk read metrics found for node %s, using default values", nodeName)
			stats.DiskReadTotal = 500 * 1024 * 1024 // 500 MB/s
			stats.DiskReadFree = 300 * 1024 * 1024  // 300 MB/s
		} else {
			// Set total capacity at 3x the current rate (estimation)
			stats.DiskReadTotal = 3 * diskReadTotal
			// Free capacity is total minus the current rate
			stats.DiskReadFree = stats.DiskReadTotal - diskReadTotal
			if stats.DiskReadFree < 0 {
				stats.DiskReadFree = 0
			}
		}
	}

	// Query disk write rate (bytes per second)
	diskWriteQuery := "rate(node_disk_written_bytes_total[5m])"
	diskWriteMetrics, err := QueryPrometheus(diskWriteQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch disk write metrics: %v", err)
		stats.DiskWriteTotal = 200 * 1024 * 1024 // 200 MB/s default
		stats.DiskWriteFree = 100 * 1024 * 1024  // 100 MB/s default
	} else {
		diskWriteTotal := 0.0

		// Sum up disk write rates across all disks for this node
		for instance, writeRate := range diskWriteMetrics {
			if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
				diskWriteTotal += writeRate
			}
		}

		if diskWriteTotal == 0 && len(diskWriteMetrics) > 0 {
			// If node not found but metrics exist, use first available
			for _, writeRate := range diskWriteMetrics {
				diskWriteTotal = writeRate
				break
			}
		}

		if diskWriteTotal == 0 {
			log.Printf("Warning: No disk write metrics found for node %s, using default values", nodeName)
			stats.DiskWriteTotal = 200 * 1024 * 1024 // 200 MB/s
			stats.DiskWriteFree = 100 * 1024 * 1024  // 100 MB/s
		} else {
			// Set total capacity at 3x the current rate (estimation)
			stats.DiskWriteTotal = 3 * diskWriteTotal
			// Free capacity is total minus the current rate
			stats.DiskWriteFree = stats.DiskWriteTotal - diskWriteTotal
			if stats.DiskWriteFree < 0 {
				stats.DiskWriteFree = 0
			}
		}
	}

	// ---------- Network Metrics ----------
	// Query network upload rate (bytes per second)
	netUpQuery := "rate(node_network_transmit_bytes_total[5m])"
	netUpMetrics, err := QueryPrometheus(netUpQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch network upload metrics: %v", err)
		stats.NetUpTotal = 100 * 1024 * 1024 // 100 MB/s default
		stats.NetUpFree = 80 * 1024 * 1024   // 80 MB/s default
	} else {
		netUpTotal := 0.0

		// Sum up network transmit rates across all interfaces for this node
		for instance, upRate := range netUpMetrics {
			if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
				netUpTotal += upRate
			}
		}

		if netUpTotal == 0 && len(netUpMetrics) > 0 {
			// If node not found but metrics exist, use first available
			for _, upRate := range netUpMetrics {
				netUpTotal = upRate
				break
			}
		}

		if netUpTotal == 0 {
			log.Printf("Warning: No network upload metrics found for node %s, using default values", nodeName)
			stats.NetUpTotal = 100 * 1024 * 1024 // 100 MB/s
			stats.NetUpFree = 80 * 1024 * 1024   // 80 MB/s
		} else {
			// Set total capacity at 5x the current rate (estimation)
			stats.NetUpTotal = 5 * netUpTotal
			// Free capacity is total minus the current rate
			stats.NetUpFree = stats.NetUpTotal - netUpTotal
			if stats.NetUpFree < 0 {
				stats.NetUpFree = 0
			}
		}
	}

	// Query network download rate (bytes per second)
	netDownQuery := "rate(node_network_receive_bytes_total[5m])"
	netDownMetrics, err := QueryPrometheus(netDownQuery)
	if err != nil {
		log.Printf("Warning: Failed to fetch network download metrics: %v", err)
		stats.NetDownTotal = 200 * 1024 * 1024 // 200 MB/s default
		stats.NetDownFree = 150 * 1024 * 1024  // 150 MB/s default
	} else {
		netDownTotal := 0.0

		// Sum up network receive rates across all interfaces for this node
		for instance, downRate := range netDownMetrics {
			if strings.Contains(instance, nodeName) || strings.Contains(nodeName, strings.Split(instance, ":")[0]) {
				netDownTotal += downRate
			}
		}

		if netDownTotal == 0 && len(netDownMetrics) > 0 {
			// If node not found but metrics exist, use first available
			for _, downRate := range netDownMetrics {
				netDownTotal = downRate
				break
			}
		}

		if netDownTotal == 0 {
			log.Printf("Warning: No network download metrics found for node %s, using default values", nodeName)
			stats.NetDownTotal = 200 * 1024 * 1024 // 200 MB/s
			stats.NetDownFree = 150 * 1024 * 1024  // 150 MB/s
		} else {
			// Set total capacity at 5x the current rate (estimation)
			stats.NetDownTotal = 5 * netDownTotal
			// Free capacity is total minus the current rate
			stats.NetDownFree = stats.NetDownTotal - netDownTotal
			if stats.NetDownFree < 0 {
				stats.NetDownFree = 0
			}
		}
	}

	return stats, nil
}
