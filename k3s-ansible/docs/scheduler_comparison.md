# Scheduler Comparison: Preemptive vs Multi-Resource

This document outlines the key differences and similarities between the Preemptive Scheduler and the Multi-Resource Scheduler implementations.

## Common Features

Both schedulers share the following features:

- **Multi-dimensional resource awareness**: Both schedulers consider CPU, memory, disk, and network usage when making scheduling decisions.
- **Prometheus integration**: Both query Prometheus for real-time resource metrics on nodes.
- **Custom scheduling logic**: Both implement custom scoring algorithms to find the best node for each pod.
- **Kubernetes API integration**: Both use the Kubernetes API to bind pods to nodes.

## Key Differences

| Feature | Preemptive Scheduler | Multi-Resource Scheduler |
|---------|---------------------|-------------------------|
| **Preemption** | Supports preemption of lower-priority pods | Does not support preemption |
| **Priority classes** | Creates and uses priority classes | Does not use priority classes |
| **Pod eviction** | Can evict pods to make room for higher-priority pods | Never evicts pods |
| **Command-line flags** | Includes `--preemption` flag | Does not include preemption-related flags |
| **Use case** | Environments where some workloads are more critical than others | Environments where all workloads have equal importance |

## Implementation Details

### Preemptive Scheduler

The Preemptive Scheduler includes the following preemption-related functionality:

1. **Priority class creation**: Creates "low-priority" and "high-priority" priority classes in Kubernetes.
2. **Preemption logic**: Implements `attemptPreemptionMulti()` function to identify and evict lower-priority pods.
3. **Eviction mechanism**: Uses the Kubernetes Eviction API to remove pods from nodes.
4. **Preemption flag**: Includes a `--preemption` flag that can be set to true/false to enable/disable preemption.

### Multi-Resource Scheduler

The Multi-Resource Scheduler:

1. **Removes preemption code**: All preemption-related functions and logic have been removed.
2. **Simplified scheduling**: Only schedules pods when resources are available without evicting existing pods.
3. **No priority awareness**: Treats all pods equally without considering priority classes.
4. **Simplified deployment**: Deployment configuration does not include preemption-related flags.

## When to Use Each Scheduler

- **Use the Preemptive Scheduler when**:
  - You have workloads with different priority levels
  - Critical workloads should run even if it means evicting less important ones
  - You need to ensure high-priority services remain available during resource contention

- **Use the Multi-Resource Scheduler when**:
  - All workloads have equal importance
  - You want to avoid disrupting running workloads
  - You prefer predictable behavior without unexpected pod evictions
  - You want a simpler scheduler without preemption complexity

## Testing

Each scheduler has its own test script:

- `preemption_test.py`: Tests the Preemptive Scheduler's ability to preempt lower-priority pods
- `multi_resource_test.py`: Tests the Multi-Resource Scheduler's ability to schedule pods based on available resources

These test scripts can be used to verify the behavior of each scheduler and ensure they are functioning as expected. 