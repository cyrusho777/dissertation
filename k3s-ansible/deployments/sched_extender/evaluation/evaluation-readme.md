# Scheduler Extender Evaluation Guide

This guide outlines how to evaluate the performance and effectiveness of our scheduler extender that implements dominant resource sharing logic with the default Kubernetes scheduler.

## Evaluation Metrics

When evaluating a scheduler extender with dominant resource sharing logic, consider these key metrics:

1. **Resource Utilization Efficiency**: 
   - How well does the cluster utilize available resources?
   - Does the dominant resource sharing approach lead to better packing of pods?

2. **Fairness**:
   - Do pods get allocated resources fairly according to their dominant resource needs?
   - Are high-priority workloads properly prioritized?

3. **Scheduling Success Rate**:
   - What percentage of pods are successfully scheduled vs. remain pending?
   - How does this compare to the default scheduler without the extender?

4. **Scheduling Latency**:
   - How long does it take to schedule pods with the scheduler extender vs. default scheduler?
   - Is there a significant overhead introduced by the extender?

## Test Cases

### Test Case 1: Basic Resource Distribution

**Purpose**: Verify that pods are distributed according to dominant resource share principle.

```bash
# Default scheduler test
kubectl apply -f evaluation/test-case-1-pods.yaml

# After evaluating default scheduler results, clean up
kubectl delete -f evaluation/test-case-1-pods.yaml

# Now test with scheduler extender
kubectl apply -f evaluation/test-case-1-pods-with-extender.yaml
```

**Expected Results**: With the scheduler extender, pods should be distributed across nodes in a way that minimizes the dominant resource usage on any single node compared to the default scheduler.

### Test Case 2: Mixed Workload Performance

**Purpose**: Evaluate scheduler performance with diverse workloads (compute-intensive, memory-intensive, etc.)

```bash
# Default scheduler test
kubectl apply -f evaluation/test-case-2-mixed-workload.yaml

# After evaluating default scheduler results, clean up
kubectl delete -f evaluation/test-case-2-mixed-workload.yaml

# Now test with scheduler extender
kubectl apply -f evaluation/test-case-2-mixed-workload-with-extender.yaml
```

**Expected Results**: The scheduler with extender should place compute-intensive pods on nodes with more available CPU and memory-intensive pods on nodes with more available memory more efficiently than the default scheduler.

### Test Case 3: Scaling Test

**Purpose**: Assess scheduler performance under load.

```bash
# Default scheduler test
kubectl apply -f evaluation/test-case-3-scaling.yaml

# After evaluating default scheduler results, clean up
kubectl delete -f evaluation/test-case-3-scaling.yaml

# Now test with scheduler extender
kubectl apply -f evaluation/test-case-3-scaling-with-extender.yaml
```

**Expected Results**: Pods should be scheduled more efficiently across all available nodes with the extender compared to the default scheduler.

## Evaluation Script

We provide a Python script that automates gathering and analyzing these metrics:

```bash
# Run the evaluation comparing default scheduler vs scheduler with extender
python run_evaluation.py
```

This script will:
1. Deploy each test case with both the default scheduler and scheduler with extender
2. Collect metrics on resource utilization, scheduling latency, and fairness
3. Generate comparison charts and metrics summaries

## Dominant Resource Fairness Analysis

For a detailed analysis of dominant resource fairness improvements:

```bash
# After running the evaluation, analyze DRF metrics
python analyze_drf.py
```

This will provide in-depth analysis of how well the scheduler extender enforces dominant resource fairness principles compared to the default scheduler.

## Visualizing Results

The evaluation scripts generate charts comparing:
- Resource utilization by node
- Scheduling latency distribution
- Dominant share fairness metrics
- Resource distribution efficiency

These will be saved in the `results/` directory. 

## Cleaning Up

After completing the evaluation:

```bash
# Remove all test namespaces
kubectl delete namespace sched-test sched-test-extender sched-test-mixed sched-test-mixed-extender sched-test-scaling sched-test-scaling-extender
``` 