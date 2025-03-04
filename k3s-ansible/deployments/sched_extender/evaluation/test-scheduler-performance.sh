#!/bin/bash

set -e

# Colors for better readability
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Kubernetes Multi-Resource Scheduler Evaluation ===${NC}"
echo "This script will evaluate the performance of the multi-resource scheduler extender"
echo "by comparing it with the default Kubernetes scheduler."

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}Error: kubectl is not installed or not in PATH${NC}"
    exit 1
fi

# Check if the scheduler extender is deployed
if ! kubectl get deployment -n kube-system scheduler-extender &> /dev/null; then
    echo -e "${YELLOW}Warning: Scheduler extender not found in kube-system namespace${NC}"
    echo "Please deploy the scheduler extender first using the deployment script."
    read -p "Continue with evaluation anyway? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Create a test namespace
echo -e "\n${GREEN}Creating test namespace...${NC}"
kubectl create namespace scheduler-test 2>/dev/null || echo "Namespace already exists"

# Function to clean up resources
cleanup() {
    echo -e "\n${YELLOW}Cleaning up test resources...${NC}"
    kubectl delete namespace scheduler-test --wait=false
    echo "Resources scheduled for deletion"
}

# Register cleanup function to run on exit
trap cleanup EXIT

# Function to deploy pods and measure scheduling times
test_scheduling() {
    local scheduler=$1
    local description=$2
    local yaml_file=$3
    local pod_count=${4:-5}
    
    echo -e "\n${GREEN}Testing $description${NC}"
    
    # Modify the YAML to use the specified scheduler
    if [[ "$scheduler" != "default" ]]; then
        # Create a temporary file with the scheduler specified
        cat "$yaml_file" | sed "s/# schedulerName:/schedulerName: $scheduler/g" > /tmp/test-pod-$scheduler.yaml
        yaml_file="/tmp/test-pod-$scheduler.yaml"
    fi
    
    # Deploy the pods
    start_time=$(date +%s.%N)
    
    for i in $(seq 1 $pod_count); do
        kubectl apply -f "$yaml_file" -n scheduler-test
    done
    
    # Wait for all pods to be scheduled
    echo "Waiting for pods to be scheduled..."
    
    all_scheduled=false
    timeout=60  # timeout in seconds
    elapsed=0
    
    while [[ "$all_scheduled" == "false" && $elapsed -lt $timeout ]]; do
        if [[ $(kubectl get pods -n scheduler-test --no-headers | grep -v "Running\|Completed" | wc -l) -eq 0 ]]; then
            all_scheduled=true
        else
            sleep 1
            elapsed=$((elapsed + 1))
        fi
    done
    
    end_time=$(date +%s.%N)
    
    # Calculate scheduling time
    scheduling_time=$(echo "$end_time - $start_time" | bc)
    
    echo "Scheduling time with $description: $scheduling_time seconds"
    
    # Get node distribution
    echo -e "\n${YELLOW}Pod distribution across nodes:${NC}"
    kubectl get pods -n scheduler-test -o wide | awk '{print $7}' | sort | uniq -c
    
    # Clean up the pods
    kubectl delete pods --all -n scheduler-test
    
    # Return the scheduling time for comparison
    echo "$scheduling_time"
}

# Test cases
echo -e "\n${GREEN}Starting evaluation...${NC}"

# Test case 1: CPU and memory intensive workloads
echo -e "\n${YELLOW}Test Case 1: CPU and Memory Intensive Workloads${NC}"
cat > /tmp/cpu-mem-pod.yaml << EOF
apiVersion: v1
kind: Pod
metadata:
  generateName: cpu-mem-test-
spec:
  # schedulerName: multiresource-scheduler
  containers:
  - name: cpu-mem-container
    image: nginx:latest
    resources:
      requests:
        cpu: "2"
        memory: "1Gi"
      limits:
        cpu: "4"
        memory: "2Gi"
EOF

default_time=$(test_scheduling "default" "Default Scheduler" "/tmp/cpu-mem-pod.yaml")
multiresource_time=$(test_scheduling "multiresource-scheduler" "Multi-Resource Scheduler" "/tmp/cpu-mem-pod.yaml")

# Test case 2: I/O intensive workloads
echo -e "\n${YELLOW}Test Case 2: I/O Intensive Workloads${NC}"
cat > /tmp/io-pod.yaml << EOF
apiVersion: v1
kind: Pod
metadata:
  generateName: io-test-
  annotations:
    scheduler.extender/disk-read: "100M"
    scheduler.extender/disk-write: "50M"
    scheduler.extender/net-up: "20M"
    scheduler.extender/net-down: "50M"
spec:
  # schedulerName: multiresource-scheduler
  containers:
  - name: io-container
    image: mysql:latest
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1"
        memory: "1Gi"
    env:
    - name: MYSQL_ROOT_PASSWORD
      value: "test-password"
EOF

default_time2=$(test_scheduling "default" "Default Scheduler" "/tmp/io-pod.yaml")
multiresource_time2=$(test_scheduling "multiresource-scheduler" "Multi-Resource Scheduler" "/tmp/io-pod.yaml")

# Test case 3: Mixed workloads
echo -e "\n${YELLOW}Test Case 3: Mixed Workloads${NC}"
cat > /tmp/mixed-pod.yaml << EOF
apiVersion: v1
kind: Pod
metadata:
  generateName: mixed-test-
  annotations:
    scheduler.extender/disk-read: "30M"
    scheduler.extender/disk-write: "15M"
    scheduler.extender/net-up: "10M"
    scheduler.extender/net-down: "20M"
spec:
  # schedulerName: multiresource-scheduler
  containers:
  - name: mixed-container
    image: postgres:latest
    resources:
      requests:
        cpu: "1"
        memory: "1Gi"
      limits:
        cpu: "2"
        memory: "2Gi"
    env:
    - name: POSTGRES_PASSWORD
      value: "test-password"
EOF

default_time3=$(test_scheduling "default" "Default Scheduler" "/tmp/mixed-pod.yaml")
multiresource_time3=$(test_scheduling "multiresource-scheduler" "Multi-Resource Scheduler" "/tmp/mixed-pod.yaml")

# Show results
echo -e "\n${GREEN}=== Scheduling Performance Results ===${NC}"
echo -e "${YELLOW}Test Case 1: CPU and Memory Intensive Workloads${NC}"
echo "Default Scheduler time: $default_time seconds"
echo "Multi-Resource Scheduler time: $multiresource_time seconds"
improvement1=$(echo "scale=2; ($default_time - $multiresource_time) / $default_time * 100" | bc)
echo -e "Improvement: ${GREEN}$improvement1%${NC}"

echo -e "\n${YELLOW}Test Case 2: I/O Intensive Workloads${NC}"
echo "Default Scheduler time: $default_time2 seconds"
echo "Multi-Resource Scheduler time: $multiresource_time2 seconds"
improvement2=$(echo "scale=2; ($default_time2 - $multiresource_time2) / $default_time2 * 100" | bc)
echo -e "Improvement: ${GREEN}$improvement2%${NC}"

echo -e "\n${YELLOW}Test Case 3: Mixed Workloads${NC}"
echo "Default Scheduler time: $default_time3 seconds"
echo "Multi-Resource Scheduler time: $multiresource_time3 seconds"
improvement3=$(echo "scale=2; ($default_time3 - $multiresource_time3) / $default_time3 * 100" | bc)
echo -e "Improvement: ${GREEN}$improvement3%${NC}"

# Get metrics from Prometheus if available
if kubectl get svc -n monitoring prometheus-server &> /dev/null; then
    echo -e "\n${GREEN}=== Collecting Prometheus Metrics ===${NC}"
    echo "Attempting to get resource utilization metrics..."
    
    # This section can be expanded to query Prometheus for specific metrics
    # related to resource utilization across nodes
    echo "To view metrics, access the Prometheus dashboard and query for:"
    echo "- node_cpu_utilization"
    echo "- node_memory_utilization"
    echo "- node_disk_io_utilization"
    echo "- node_network_io_utilization"
fi

echo -e "\n${GREEN}Evaluation complete!${NC}"
echo "Note: This is a basic evaluation. For a more comprehensive analysis,"
echo "consider running longer tests with varied workloads and analyzing"
echo "the resource utilization metrics from Prometheus." 