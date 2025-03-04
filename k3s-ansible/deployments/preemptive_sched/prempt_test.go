package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// --------------------
// Test for canSchedule (pure function)
// --------------------
func TestCanSchedule(t *testing.T) {
	tests := []struct {
		name     string
		podReq   PodRequest
		stats    NodeStats
		alpha    float64
		expected bool
	}{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := canSchedule(tc.podReq, tc.stats, tc.alpha)
			if res != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, res)
			}
		})
	}
}

// --------------------
// Mock HTTP server for testing getNodeStats
// --------------------
func setupMockPrometheusServer(t *testing.T, data map[string]float64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		val, ok := data[query]
		if !ok {
			http.Error(w, fmt.Sprintf("query not found: %s", query), http.StatusNotFound)
			return
		}

		// Create a response in Prometheus API format
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []interface{}{
					map[string]interface{}{
						"metric": map[string]string{},
						"value": []interface{}{
							float64(time.Now().Unix()),
							fmt.Sprintf("%f", val),
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
}

func TestGetNodeStats(t *testing.T) {
	fakeData := map[string]float64{
		// CPU metrics with different label formats
		`machine_cpu_cores{kubernetes_io_hostname="node1"}`:                                   4.0,
		`sum(rate(node_cpu_seconds_total{mode!="idle", kubernetes_io_hostname="node1"}[1m]))`: 2.0,
		`node_memory_MemTotal_bytes{kubernetes_io_hostname="node1"}`:                          4 * 1024 * 1024 * 1024,
		`node_memory_MemAvailable_bytes{kubernetes_io_hostname="node1"}`:                      2 * 1024 * 1024 * 1024,
	}

	// Setup a mock Prometheus server
	server := setupMockPrometheusServer(t, fakeData)
	defer server.Close()

	// Temporarily override the getPrometheusURL function
	originalGetPrometheusURL := getPrometheusURL
	defer func() { getPrometheusURL = originalGetPrometheusURL }()
	getPrometheusURL = func() string {
		return server.URL
	}

	// Call the function under test
	stats, err := getNodeStats("node1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := NodeStats{
		CPUFree:  2.0,
		CPUTotal: 4.0,
		MemFree:  2 * 1024 * 1024 * 1024,
		MemTotal: 4 * 1024 * 1024 * 1024,
	}
	if !reflect.DeepEqual(stats, expected) {
		t.Errorf("Expected %+v, got %+v", expected, stats)
	}
}

// --------------------
// Test for attemptPreemption using fake clientset.
// --------------------
func TestAttemptPreemption(t *testing.T) {
	// Create a fake clientset.
	clientset := fake.NewSimpleClientset()

	// Create two low-priority pods running on "node1".
	lowPod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "low-pod-1",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "node1",
			Containers: []v1.Container{
				{
					Name:  "container",
					Image: "busybox",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0.5"),
							v1.ResourceMemory: resource.MustParse("500Mi"),
						},
					},
				},
			},
			Priority: int32Ptr(1),
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}
	lowPod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "low-pod-2",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			NodeName: "node1",
			Containers: []v1.Container{
				{
					Name:  "container",
					Image: "busybox",
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0.5"),
							v1.ResourceMemory: resource.MustParse("500Mi"),
						},
					},
				},
			},
			Priority: int32Ptr(1),
		},
		Status: v1.PodStatus{
			Phase: v1.PodRunning,
		},
	}
	_, err := clientset.CoreV1().Pods("default").Create(context.Background(), lowPod1, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create fake pod: %v", err)
	}
	_, err = clientset.CoreV1().Pods("default").Create(context.Background(), lowPod2, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create fake pod: %v", err)
	}

	// Set up a reactor on evictions to simulate successful eviction.
	clientset.Fake.PrependReactor("create", "evictions", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil
	})

	// Define a high-priority pod request that requires 1 CPU and 1Gi memory.
	podReq := PodRequest{
		CPU:      1.0,
		Mem:      1024 * 1024 * 1024, // 1GiB
		Priority: 10,
	}
	// Simulate a scenario where the node initially has insufficient free resources.
	stats := NodeStats{
		CPUFree:  0.5, // Only 0.5 CPU free.
		CPUTotal: 4.0,
		MemFree:  512 * 1024 * 1024, // 512Mi free.
		MemTotal: 4 * 1024 * 1024 * 1024,
	}

	// Attempt preemption.
	success := attemptPreemption(clientset, podReq, "node1", stats)
	if !success {
		t.Errorf("Expected preemption to succeed, but it failed")
	}
}

// Helper to get a pointer to int32.
func int32Ptr(i int) *int32 {
	v := int32(i)
	return &v
}
