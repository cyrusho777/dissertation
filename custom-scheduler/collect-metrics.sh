#!/bin/bash

# Start timing
START_TIME=$(date +%s)

# Apply the standard scheduler
kubectl apply -f standard-scheduler-configmap.yaml
kubectl apply -f standard-scheduler-deployment.yaml

echo "Waiting for standard scheduler to start..."
sleep 10

# Check if both schedulers are running
kubectl get pods -n kube-system -l app=standard-scheduler
kubectl get pods -n kube-system -l app=custom-multiresource-scheduler

# Apply some test pods
echo "Creating individual test pods..."
kubectl apply -f test-standard-pod.yaml
kubectl apply -f test-multiresource-pod.yaml

# Wait for pods to be scheduled
echo "Waiting for individual pods to be scheduled..."
sleep 5

# Check the pod status
kubectl get pod test-standard-pod -o wide
kubectl get pod test-multiresource-pod -o wide

# Run load tests
echo "Starting load test..."
kubectl apply -f test-load-generator.yaml

# Wait for jobs to complete
echo "Waiting for load test to complete..."
sleep 60

# Collect metrics
echo "Collecting scheduling metrics..."

echo "=== MultiResource Scheduler Pod Distribution ==="
kubectl get pods -l test=multiresource-load -o wide | awk '{print $7}' | sort | uniq -c | sort -nr

echo "=== Standard Scheduler Pod Distribution ==="
kubectl get pods -l test=standard-load -o wide | awk '{print $7}' | sort | uniq -c | sort -nr

# Calculate scheduling latency using created timestamps
echo "=== MultiResource Scheduler Latency ==="
for pod in $(kubectl get pods -l test=multiresource-load -o name); do
  name=$(echo $pod | cut -d/ -f2)
  created=$(kubectl get $pod -o jsonpath='{.metadata.creationTimestamp}')
  scheduled=$(kubectl get $pod -o jsonpath='{.status.conditions[?(@.type=="PodScheduled")].lastTransitionTime}')
  created_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$created" "+%s" 2>/dev/null || date -d "$created" "+%s")
  scheduled_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$scheduled" "+%s" 2>/dev/null || date -d "$scheduled" "+%s")
  latency=$((scheduled_epoch - created_epoch))
  echo "$name: $latency seconds"
done

echo "=== Standard Scheduler Latency ==="
for pod in $(kubectl get pods -l test=standard-load -o name); do
  name=$(echo $pod | cut -d/ -f2)
  created=$(kubectl get $pod -o jsonpath='{.metadata.creationTimestamp}')
  scheduled=$(kubectl get $pod -o jsonpath='{.status.conditions[?(@.type=="PodScheduled")].lastTransitionTime}')
  created_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$created" "+%s" 2>/dev/null || date -d "$created" "+%s")
  scheduled_epoch=$(date -j -f "%Y-%m-%dT%H:%M:%SZ" "$scheduled" "+%s" 2>/dev/null || date -d "$scheduled" "+%s")
  latency=$((scheduled_epoch - created_epoch))
  echo "$name: $latency seconds"
done

# Calculate total time
END_TIME=$(date +%s)
TOTAL_TIME=$((END_TIME - START_TIME))
echo "Total test time: $TOTAL_TIME seconds"

# Cleanup
echo "Do you want to cleanup the test resources? (y/n)"
read -r answer
if [[ "$answer" == "y" ]]; then
  kubectl delete -f test-load-generator.yaml
  kubectl delete pods -l test=multiresource-load
  kubectl delete pods -l test=standard-load
  kubectl delete -f test-standard-pod.yaml
  kubectl delete -f test-multiresource-pod.yaml
  kubectl delete -f standard-scheduler-deployment.yaml
  kubectl delete -f standard-scheduler-configmap.yaml
  echo "Cleanup completed."
fi 