package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/gookit/color"
)

func getPrometheusURL() string {
	baseUrl := os.Getenv("PROMETHEUS_URL")
	if baseUrl == "" {
		baseUrl = "http://prometheus-server.default.svc.cluster.local:80/api/v1/query"
	}
	return baseUrl
}

// Query Prometheus API and return metrics as a map
func queryPrometheus(query string) (map[string]float64, error) {
	prometheusURL := getPrometheusURL()
	// Encode query to avoid issues with special characters
	args := url.Values{}
	args.Add("query", query)
	// Construct final request URL
	constructedURL := fmt.Sprintf("%s?%s", prometheusURL, args.Encode())

	// Print statement to debug the full URL query. Compare w cURL
	// fmt.Println("Debug: Requesting URL ->", constructedURL)

	// Make HTTP request
	// Create HTTP request
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Prevent redirects
		},
	}
	req, err := http.NewRequest("GET", constructedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating HTTP request: %w", err)
	}

	// Set JSON headers
	req.Header.Set("Accept", "application/json")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making GET request: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}
	// Debug print statement for the response body
	// fmt.Println("Response Body:", string(body))

	var result struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	metrics := make(map[string]float64)
	for _, r := range result.Data.Result {
		node := r.Metric["instance"]
		// Ensure the "value" field exists and has at least 2 elements
		if len(r.Value) < 2 {
			fmt.Printf("Skipping invalid metric entry: %+v\n", r)
			continue
		}
		valueStr, ok := r.Value[1].(string)
		if !ok {
			return nil, fmt.Errorf("unexpected value type: %v", r.Value[1])
		}
		metrics[node] = parseFloat(valueStr)
	}

	return metrics, nil
}

// Convert string to float
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// Display metrics in the terminal
func displayMetrics() {
	for {
		fmt.Println("\n===== Kubernetes Cluster Metrics =====")

		// Query and print CPU usage per node
		cpuMetrics, err := queryPrometheus("sum(rate(node_cpu_seconds_total{mode!=\"idle\"}[5m])) by (instance)")
		if err == nil {
			fmt.Println("\n CPU Usage Per Node (cores):")
			for node, usage := range cpuMetrics {
				color.Info.Printf("  - Node: %s, CPU: %.2f cores\n", node, usage)
			}
		} else {
			color.Warn.Println("⚠️ Failed to fetch CPU metrics:", err)
		}

		// Query and print Memory usage per node
		memMetrics, err := queryPrometheus("node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes")
		if err == nil {
			fmt.Println("\n Memory Usage Per Node (bytes):")
			for node, usage := range memMetrics {
				color.Info.Printf("  - Node: %s, Memory Used: %.2f GB\n", node, usage/(1024*1024*1024))
			}
		} else {
			color.Warn.Println("⚠️ Failed to fetch Memory metrics:", err)
		}

		// Query and print Disk usage per node
		diskMetrics, err := queryPrometheus("node_filesystem_size_bytes{mountpoint=\"/\"} - node_filesystem_avail_bytes{mountpoint=\"/\"}")
		if err == nil {
			fmt.Println("\n Disk Usage Per Node (bytes):")
			for node, usage := range diskMetrics {
				color.Info.Printf("  - Node: %s, Disk Used: %.2f GB\n", node, usage/(1024*1024*1024))
			}
		} else {
			color.Warn.Println("⚠️ Failed to fetch Disk metrics:", err)
		}

		// Query and print Network traffic per node
		netInMetrics, err := queryPrometheus("rate(node_network_receive_bytes_total[5m])")
		if err == nil {
			fmt.Println("\n Network Incoming Traffic Per Node (bytes/sec):")
			for node, usage := range netInMetrics {
				color.Info.Printf("  - Node: %s, Incoming: %.2f KB/s\n", node, usage/1024)
			}
		}

		netOutMetrics, err := queryPrometheus("rate(node_network_transmit_bytes_total[5m])")
		if err == nil {
			fmt.Println("\n Network Outgoing Traffic Per Node (bytes/sec):")
			for node, usage := range netOutMetrics {
				color.Info.Printf("  - Node: %s, Outgoing: %.2f KB/s\n", node, usage/1024)
			}
		}

		fmt.Println("\n======================================\n")

		time.Sleep(10 * time.Second) // Refresh every 10 seconds
	}
}

func main() {
	fmt.Println("Prometheus metrics experimental tool")
	displayMetrics()
}
