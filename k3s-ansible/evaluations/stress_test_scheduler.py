#!/usr/bin/env python3
import time
import datetime
import argparse
import matplotlib.pyplot as plt
import pandas as pd
import numpy as np
from kubernetes import client, config
import os
import json
import random
import uuid

# Configure command-line arguments.
parser = argparse.ArgumentParser(description="Stress test scheduler performance with realistic workloads.")
parser.add_argument("--namespace", default="default", help="Kubernetes namespace to use")
parser.add_argument("--poll-interval", type=int, default=2, help="Seconds between polling for pod status")
parser.add_argument("--timeout", type=int, default=900, help="Overall timeout in seconds for the test")
parser.add_argument("--compare", action="store_true", help="Run tests for both schedulers and compare results")
parser.add_argument("--scheduler", choices=["default", "preemptive"], default="default", 
                    help="Which scheduler to use if not comparing both")
parser.add_argument("--output-file", default="stress_test_results.json", help="File to save results to")
parser.add_argument("--load-results", action="store_true", help="Load and plot previous results instead of running new tests")
parser.add_argument("--background-load", type=int, default=5, 
                    help="Number of background pods to create to simulate cluster load")
parser.add_argument("--high-priority-pods", type=int, default=3, 
                    help="Number of high priority pods to create")
parser.add_argument("--medium-priority-pods", type=int, default=5, 
                    help="Number of medium priority pods to create")
parser.add_argument("--low-priority-pods", type=int, default=10, 
                    help="Number of low priority pods to create")
parser.add_argument("--resource-contention", choices=["low", "medium", "high"], default="medium",
                    help="Level of resource contention to simulate")
args = parser.parse_args()

# Load Kubernetes configuration.
try:
    config.load_kube_config()  # for local testing
except:
    config.load_incluster_config()  # when running inside cluster
v1 = client.CoreV1Api()

# Define resource profiles based on contention level
RESOURCE_PROFILES = {
    "low": {
        "background": {"cpu": "100m", "memory": "128Mi"},
        "low_priority": {"cpu": "200m", "memory": "256Mi"},
        "medium_priority": {"cpu": "300m", "memory": "384Mi"},
        "high_priority": {"cpu": "400m", "memory": "512Mi"}
    },
    "medium": {
        "background": {"cpu": "200m", "memory": "256Mi"},
        "low_priority": {"cpu": "300m", "memory": "384Mi"},
        "medium_priority": {"cpu": "500m", "memory": "512Mi"},
        "high_priority": {"cpu": "700m", "memory": "768Mi"}
    },
    "high": {
        "background": {"cpu": "300m", "memory": "384Mi"},
        "low_priority": {"cpu": "500m", "memory": "512Mi"},
        "medium_priority": {"cpu": "700m", "memory": "768Mi"},
        "high_priority": {"cpu": "1000m", "memory": "1Gi"}
    }
}

# Priority values
PRIORITIES = {
    "low": 10,
    "medium": 50,
    "high": 90
}

def create_priority_class(name, value, description):
    """Create a priority class if it doesn't exist."""
    try:
        # Check if priority class exists
        client.SchedulingV1Api().read_priority_class(name=name)
    except:
        # Create priority class if it doesn't exist
        priority_class = client.V1PriorityClass(
            metadata=client.V1ObjectMeta(name=name),
            value=value,
            global_default=False,
            description=description
        )
        client.SchedulingV1Api().create_priority_class(body=priority_class)
        print(f"Created priority class: {name} with value {value}")

def setup_priority_classes():
    """Set up the priority classes needed for the test."""
    create_priority_class("low-priority", PRIORITIES["low"], "Low priority pods")
    create_priority_class("medium-priority", PRIORITIES["medium"], "Medium priority pods")
    create_priority_class("high-priority", PRIORITIES["high"], "High priority pods")

def get_stress_command(resource_type, intensity):
    """Generate a command to stress a specific resource."""
    if resource_type == "cpu":
        # Stress CPU with varying intensity
        if intensity == "low":
            return ["stress", "--cpu", "1", "--timeout", "300"]
        elif intensity == "medium":
            return ["stress", "--cpu", "2", "--timeout", "300"]
        else:  # high
            return ["stress", "--cpu", "4", "--timeout", "300"]
    elif resource_type == "memory":
        # Stress memory with varying intensity
        if intensity == "low":
            return ["stress", "--vm", "1", "--vm-bytes", "128M", "--timeout", "300"]
        elif intensity == "medium":
            return ["stress", "--vm", "2", "--vm-bytes", "256M", "--timeout", "300"]
        else:  # high
            return ["stress", "--vm", "3", "--vm-bytes", "512M", "--timeout", "300"]
    elif resource_type == "io":
        # Stress I/O with varying intensity
        if intensity == "low":
            return ["stress", "--io", "1", "--timeout", "300"]
        elif intensity == "medium":
            return ["stress", "--io", "2", "--timeout", "300"]
        else:  # high
            return ["stress", "--io", "4", "--timeout", "300"]
    else:
        # Default to a mix of stressors
        return ["stress", "--cpu", "1", "--vm", "1", "--vm-bytes", "128M", "--timeout", "300"]

def submit_pod(pod_name, namespace, image, command, scheduler_name=None, 
               cpu_request="100m", memory_request="128Mi", priority_class=None,
               labels=None, annotations=None):
    """Create a pod with the given specifications."""
    
    # Create pod manifest
    pod_manifest = client.V1Pod(
        metadata=client.V1ObjectMeta(
            name=pod_name,
            labels=labels or {},
            annotations=annotations or {}
        ),
        spec=client.V1PodSpec(
            scheduler_name=scheduler_name,  # Use specified scheduler or default if None
            restart_policy="Never",
            priority_class_name=priority_class,
            tolerations=[
                client.V1Toleration(
                    key="node-role.kubernetes.io/master",
                    operator="Equal",
                    value="true",
                    effect="NoSchedule"
                )
            ],
            containers=[
                client.V1Container(
                    name="container",
                    image=image,
                    command=command,
                    resources=client.V1ResourceRequirements(
                        requests={"cpu": cpu_request, "memory": memory_request},
                        limits={"cpu": cpu_request, "memory": memory_request}
                    )
                )
            ]
        )
    )
    try:
        v1.create_namespaced_pod(namespace=namespace, body=pod_manifest)
        return True
    except Exception as e:
        print(f"Error creating pod {pod_name}: {e}")
        return False

def get_pod_status(pod_name, namespace):
    """Get the current status of a pod."""
    try:
        pod = v1.read_namespaced_pod(name=pod_name, namespace=namespace)
        return {
            "name": pod_name,
            "phase": pod.status.phase,
            "start_time": pod.status.start_time,
            "conditions": pod.status.conditions,
            "node_name": pod.spec.node_name
        }
    except Exception as e:
        print(f"Error reading pod {pod_name}: {e}")
        return None

def create_background_load(namespace, scheduler_name, num_pods, resource_profile):
    """Create background pods to simulate cluster load."""
    print(f"Creating {num_pods} background pods with {scheduler_name or 'default'} scheduler")
    
    background_pods = []
    for i in range(num_pods):
        pod_name = f"background-{i}-{uuid.uuid4().hex[:6]}"
        
        # Randomly select a stress type
        stress_type = random.choice(["cpu", "memory", "io", "mixed"])
        stress_intensity = random.choice(["low", "medium", "high"])
        
        if stress_type == "mixed":
            command = ["stress", "--cpu", "1", "--vm", "1", "--vm-bytes", "128M", "--io", "1", "--timeout", "600"]
        else:
            command = get_stress_command(stress_type, stress_intensity)
        
        cpu_request = resource_profile["cpu"]
        memory_request = resource_profile["memory"]
        
        labels = {
            "app": "scheduler-test",
            "role": "background",
            "scheduler": scheduler_name or "default"
        }
        
        success = submit_pod(
            pod_name=pod_name,
            namespace=namespace,
            image="polinux/stress",
            command=command,
            scheduler_name=scheduler_name,
            cpu_request=cpu_request,
            memory_request=memory_request,
            labels=labels
        )
        
        if success:
            background_pods.append(pod_name)
    
    return background_pods

def create_priority_pods(namespace, scheduler_name, priority_level, num_pods, resource_profile):
    """Create pods with a specific priority level."""
    print(f"Creating {num_pods} {priority_level} priority pods with {scheduler_name or 'default'} scheduler")
    
    priority_pods = []
    for i in range(num_pods):
        pod_name = f"{priority_level}-priority-{i}-{uuid.uuid4().hex[:6]}"
        
        # Determine stress parameters based on priority
        if priority_level == "high":
            stress_type = random.choice(["cpu", "memory", "mixed"])
            stress_intensity = "high"
        elif priority_level == "medium":
            stress_type = random.choice(["cpu", "memory", "io", "mixed"])
            stress_intensity = "medium"
        else:  # low
            stress_type = random.choice(["cpu", "memory", "io", "mixed"])
            stress_intensity = "low"
        
        if stress_type == "mixed":
            command = ["stress", "--cpu", "1", "--vm", "1", "--vm-bytes", "128M", "--timeout", "300"]
        else:
            command = get_stress_command(stress_type, stress_intensity)
        
        cpu_request = resource_profile["cpu"]
        memory_request = resource_profile["memory"]
        
        labels = {
            "app": "scheduler-test",
            "role": priority_level,
            "scheduler": scheduler_name or "default"
        }
        
        success = submit_pod(
            pod_name=pod_name,
            namespace=namespace,
            image="polinux/stress",
            command=command,
            scheduler_name=scheduler_name,
            cpu_request=cpu_request,
            memory_request=memory_request,
            priority_class=f"{priority_level}-priority",
            labels=labels
        )
        
        if success:
            priority_pods.append(pod_name)
    
    return priority_pods

def monitor_pods(namespace, pod_names, poll_interval, timeout):
    """Monitor pods until they are all scheduled or timeout is reached."""
    print(f"Monitoring {len(pod_names)} pods...")
    
    pod_statuses = {pod: {"scheduled": False, "start_time": None} for pod in pod_names}
    submission_time = datetime.datetime.now(datetime.timezone.utc)
    
    end_time = time.time() + timeout
    while time.time() < end_time:
        all_scheduled = True
        
        for pod_name in pod_names:
            if not pod_statuses[pod_name]["scheduled"]:
                status = get_pod_status(pod_name, namespace)
                
                if status and status["phase"] == "Running" and status["start_time"]:
                    pod_statuses[pod_name]["scheduled"] = True
                    pod_statuses[pod_name]["start_time"] = status["start_time"]
                    pod_statuses[pod_name]["node"] = status["node_name"]
                    print(f"Pod {pod_name} scheduled at {status['start_time']} on node {status['node_name']}")
                else:
                    all_scheduled = False
        
        if all_scheduled:
            print("All pods scheduled successfully!")
            break
        
        time.sleep(poll_interval)
    
    # Calculate metrics
    scheduled_count = sum(1 for pod in pod_statuses.values() if pod["scheduled"])
    
    if scheduled_count < len(pod_names):
        print(f"Timeout reached. Only {scheduled_count} pods were scheduled out of {len(pod_names)}.")
    
    # Calculate latencies
    latencies = {}
    for pod_name, status in pod_statuses.items():
        if status["scheduled"] and status["start_time"]:
            latency = (status["start_time"] - submission_time).total_seconds()
            latencies[pod_name] = latency
    
    # Calculate makespan (time from first to last pod scheduled)
    if latencies:
        start_times = [status["start_time"] for status in pod_statuses.values() 
                      if status["scheduled"] and status["start_time"]]
        if start_times:
            makespan = (max(start_times) - min(start_times)).total_seconds()
        else:
            makespan = 0
    else:
        makespan = 0
    
    metrics = {
        "scheduled_count": scheduled_count,
        "total_count": len(pod_names),
        "latencies": latencies,
        "makespan": makespan,
        "avg_latency": sum(latencies.values()) / len(latencies) if latencies else 0,
        "median_latency": np.median(list(latencies.values())) if latencies else 0,
        "p95_latency": np.percentile(list(latencies.values()), 95) if latencies else 0,
        "p99_latency": np.percentile(list(latencies.values()), 99) if latencies else 0,
        "pod_statuses": pod_statuses
    }
    
    return metrics

def cleanup_pods(namespace, label_selector="app=scheduler-test"):
    """Delete all test pods."""
    print(f"Cleaning up test pods with label: {label_selector}")
    try:
        v1.delete_collection_namespaced_pod(namespace=namespace, label_selector=label_selector)
        # Wait for pods to be deleted
        time.sleep(10)
        print("Cleanup completed.")
    except Exception as e:
        print(f"Error during cleanup: {e}")

def run_stress_test(scheduler_name=None):
    """Run a stress test with the specified scheduler."""
    test_id = uuid.uuid4().hex[:8]
    print(f"\n===== TESTING {scheduler_name or 'DEFAULT'} SCHEDULER (ID: {test_id}) =====")
    
    # Set up priority classes
    setup_priority_classes()
    
    # Get resource profiles based on contention level
    resource_profiles = RESOURCE_PROFILES[args.resource_contention]
    
    # Create background load
    background_pods = create_background_load(
        namespace=args.namespace,
        scheduler_name=scheduler_name,
        num_pods=args.background_load,
        resource_profile=resource_profiles["background"]
    )
    
    # Wait for background pods to start
    print("Waiting for background pods to start...")
    time.sleep(15)  # Reduced from 30 to 15 seconds
    
    # Create all priority pods at once to better test scheduling decisions
    print(f"Creating pods with priorities (high: {args.high_priority_pods}, medium: {args.medium_priority_pods}, low: {args.low_priority_pods})")
    
    # Create low priority pods
    low_priority_pods = create_priority_pods(
        namespace=args.namespace,
        scheduler_name=scheduler_name,
        priority_level="low",
        num_pods=args.low_priority_pods,
        resource_profile=resource_profiles["low_priority"]
    )
    
    # Create medium priority pods
    medium_priority_pods = create_priority_pods(
        namespace=args.namespace,
        scheduler_name=scheduler_name,
        priority_level="medium",
        num_pods=args.medium_priority_pods,
        resource_profile=resource_profiles["medium_priority"]
    )
    
    # Create high priority pods
    high_priority_pods = create_priority_pods(
        namespace=args.namespace,
        scheduler_name=scheduler_name,
        priority_level="high",
        num_pods=args.high_priority_pods,
        resource_profile=resource_profiles["high_priority"]
    )
    
    # Combine all priority pods for monitoring
    all_priority_pods = low_priority_pods + medium_priority_pods + high_priority_pods
    
    # Monitor pods
    metrics = monitor_pods(
        namespace=args.namespace,
        pod_names=all_priority_pods,
        poll_interval=args.poll_interval,
        timeout=args.timeout
    )
    
    # Add test metadata
    metrics["scheduler"] = scheduler_name or "default"
    metrics["test_id"] = test_id
    metrics["resource_contention"] = args.resource_contention
    metrics["background_pods"] = len(background_pods)
    metrics["low_priority_pods"] = len(low_priority_pods)
    metrics["medium_priority_pods"] = len(medium_priority_pods)
    metrics["high_priority_pods"] = len(high_priority_pods)
    
    # Cleanup
    cleanup_pods(args.namespace, f"app=scheduler-test,scheduler={scheduler_name or 'default'}")
    
    return metrics

def plot_comparison(default_metrics, preemptive_metrics):
    """Plot comparison between default and preemptive schedulers."""
    # Create DataFrames for latencies by priority
    default_latencies = default_metrics["latencies"]
    preemptive_latencies = preemptive_metrics["latencies"]
    
    default_df = pd.DataFrame(list(default_latencies.items()), columns=["Pod", "Latency"])
    default_df["Scheduler"] = "Default Scheduler"
    default_df["Priority"] = default_df["Pod"].apply(lambda x: x.split("-")[0])
    
    preemptive_df = pd.DataFrame(list(preemptive_latencies.items()), columns=["Pod", "Latency"])
    preemptive_df["Scheduler"] = "Preemptive Scheduler"
    preemptive_df["Priority"] = preemptive_df["Pod"].apply(lambda x: x.split("-")[0])
    
    # Combine data
    combined_df = pd.concat([default_df, preemptive_df])
    
    # 1. Histogram of scheduling latency
    plt.figure(figsize=(12, 6))
    plt.hist([default_df["Latency"], preemptive_df["Latency"]], bins=20, 
             label=["Default Scheduler", "Preemptive Scheduler"],
             alpha=0.7, edgecolor='black')
    plt.xlabel("Scheduling Latency (seconds)")
    plt.ylabel("Number of Pods")
    plt.title("Distribution of Pod Scheduling Latency")
    plt.legend()
    plt.grid(True, alpha=0.3)
    plt.savefig("latency_histogram.png")
    
    # 2. Bar chart of average latency by priority
    plt.figure(figsize=(12, 6))
    
    # Group by scheduler and priority
    priority_latencies = combined_df.groupby(["Scheduler", "Priority"])["Latency"].mean().reset_index()
    
    # Pivot for easier plotting
    pivot_df = priority_latencies.pivot(index="Priority", columns="Scheduler", values="Latency")
    
    # Sort by priority
    priority_order = ["high", "medium", "low"]
    pivot_df = pivot_df.reindex(priority_order)
    
    # Plot
    ax = pivot_df.plot(kind="bar", figsize=(12, 6))
    plt.xlabel("Pod Priority")
    plt.ylabel("Average Scheduling Latency (seconds)")
    plt.title("Average Scheduling Latency by Pod Priority")
    plt.grid(True, alpha=0.3)
    
    # Add value labels
    for i, (idx, row) in enumerate(pivot_df.iterrows()):
        for j, col in enumerate(pivot_df.columns):
            value = row[col]
            ax.text(i + (j-0.5)*0.4, value + 0.1, f"{value:.2f}s", ha='center')
    
    plt.savefig("priority_latency.png")
    
    # 3. Scheduling success rate
    plt.figure(figsize=(12, 6))
    
    # Calculate success rates
    default_success = default_metrics["scheduled_count"] / default_metrics["total_count"] * 100
    preemptive_success = preemptive_metrics["scheduled_count"] / preemptive_metrics["total_count"] * 100
    
    plt.bar(["Default Scheduler", "Preemptive Scheduler"], [default_success, preemptive_success])
    plt.xlabel("Scheduler")
    plt.ylabel("Scheduling Success Rate (%)")
    plt.title("Pod Scheduling Success Rate")
    plt.ylim(0, 105)  # Add some space at the top
    
    # Add value labels
    plt.text(0, default_success + 2, f"{default_success:.1f}%", ha='center')
    plt.text(1, preemptive_success + 2, f"{preemptive_success:.1f}%", ha='center')
    
    plt.grid(True, alpha=0.3)
    plt.savefig("success_rate.png")
    
    # Print summary
    print("\n===== PERFORMANCE COMPARISON =====")
    print(f"{'Metric':<20} {'Default Scheduler':<20} {'Preemptive Scheduler':<20} {'Improvement':<15}")
    print("-" * 75)
    
    metrics = ["avg_latency", "median_latency", "p95_latency", "p99_latency", "makespan"]
    
    for metric in metrics:
        default_val = default_metrics[metric]
        preemptive_val = preemptive_metrics[metric]
        
        if default_val == 0:
            # Handle division by zero
            improvement = 0 if preemptive_val == 0 else float('inf') if preemptive_val < 0 else float('-inf')
        else:
            improvement = ((default_val - preemptive_val) / default_val) * 100
            
        print(f"{metric:<20} {default_val:<20.2f} {preemptive_val:<20.2f} {improvement:<15.2f}%")
    
    print(f"{'Scheduled Pods':<20} {default_metrics['scheduled_count']:<20} {preemptive_metrics['scheduled_count']:<20}")
    
    # Calculate overall performance score (lower is better)
    default_score = (
        default_metrics["avg_latency"] * 0.3 + 
        default_metrics["p95_latency"] * 0.3 + 
        default_metrics["makespan"] * 0.2 +
        (1 - default_metrics["scheduled_count"] / default_metrics["total_count"]) * 100 * 0.2
    )
    
    preemptive_score = (
        preemptive_metrics["avg_latency"] * 0.3 + 
        preemptive_metrics["p95_latency"] * 0.3 + 
        preemptive_metrics["makespan"] * 0.2 +
        (1 - preemptive_metrics["scheduled_count"] / preemptive_metrics["total_count"]) * 100 * 0.2
    )
    
    if default_score == 0:
        score_improvement = 0 if preemptive_score == 0 else float('inf') if preemptive_score < 0 else float('-inf')
    else:
        score_improvement = ((default_score - preemptive_score) / default_score) * 100
        
    print(f"{'Overall Score':<20} {default_score:<20.2f} {preemptive_score:<20.2f} {score_improvement:<15.2f}%")

def save_results(results, filename):
    """Save test results to a file."""
    # Convert datetime objects to strings for JSON serialization
    for scheduler, metrics in results.items():
        if "pod_statuses" in metrics:
            for pod, status in metrics["pod_statuses"].items():
                if status["start_time"]:
                    status["start_time"] = status["start_time"].isoformat()
    
    with open(filename, 'w') as f:
        json.dump(results, f, indent=2)
    print(f"Results saved to {filename}")

def main():
    """Main function to run the stress test."""
    try:
        if args.load_results:
            # Load previous results and plot
            with open(args.output_file, 'r') as f:
                results = json.load(f)
            
            if "default" in results and "preemptive" in results:
                plot_comparison(results["default"], results["preemptive"])
            else:
                print("Error: Results file does not contain both schedulers' data.")
            return
        
        # Initialize results dictionary
        results = {}
        
        if args.compare:
            # 1. Default scheduler test
            print("\n===== TESTING DEFAULT SCHEDULER =====")
            default_metrics = run_stress_test(scheduler_name=None)
            results["default"] = default_metrics
            
            # Save intermediate results
            save_results({"default": default_metrics}, "default_" + args.output_file)
            
            # Wait between tests
            time.sleep(15)  # Reduced from 30 to 15 seconds
            
            # 2. Preemptive scheduler test
            print("\n===== TESTING PREEMPTIVE SCHEDULER =====")
            preemptive_metrics = run_stress_test(scheduler_name="preemptive-scheduler")
            results["preemptive"] = preemptive_metrics
            
            # Plot comparison
            plot_comparison(default_metrics, preemptive_metrics)
        else:
            # Run test for single scheduler
            scheduler_name = "preemptive-scheduler" if args.scheduler == "preemptive" else None
            display_name = "Preemptive Scheduler" if args.scheduler == "preemptive" else "Default Scheduler"
            
            print(f"\n===== TESTING {display_name.upper()} =====")
            metrics = run_stress_test(scheduler_name=scheduler_name)
            results[args.scheduler] = metrics
        
        # Save results
        save_results(results, args.output_file)
        
        # Final cleanup
        cleanup_pods(args.namespace)
    
    except KeyboardInterrupt:
        print("\nTest interrupted by user. Cleaning up...")
        cleanup_pods(args.namespace)
        print("Cleanup completed. Exiting.")
    except Exception as e:
        print(f"\nError during test: {e}")
        print("Cleaning up...")
        cleanup_pods(args.namespace)
        print("Cleanup completed. Exiting.")

if __name__ == "__main__":
    main() 