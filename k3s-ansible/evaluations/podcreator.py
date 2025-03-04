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

# Configure command-line arguments.
parser = argparse.ArgumentParser(description="Evaluate and compare scheduler performance by submitting many pods and recording scheduling times.")
parser.add_argument("--namespace", default="default", help="Kubernetes namespace to use")
parser.add_argument("--image", default="busybox", help="Container image to use for test pods")
parser.add_argument("--command", nargs="+", default=["sleep", "60"], help="Command to run in each pod")
parser.add_argument("--num-pods", type=int, default=100, help="Number of pods to submit per scheduler")
parser.add_argument("--poll-interval", type=int, default=2, help="Seconds between polling for pod status")
parser.add_argument("--timeout", type=int, default=600, help="Overall timeout in seconds for the test")
parser.add_argument("--compare", action="store_true", help="Run tests for both schedulers and compare results")
parser.add_argument("--scheduler", choices=["default", "preemptive"], default="default", 
                    help="Which scheduler to use if not comparing both")
parser.add_argument("--cpu-request", default="100m", help="CPU request for test pods")
parser.add_argument("--memory-request", default="128Mi", help="Memory request for test pods")
parser.add_argument("--priority", type=int, default=0, help="Priority for test pods (0-100)")
parser.add_argument("--output-file", default="scheduler_comparison.json", help="File to save results to")
parser.add_argument("--load-results", action="store_true", help="Load and plot previous results instead of running new tests")
args = parser.parse_args()

# Load Kubernetes configuration.
try:
    config.load_kube_config()  # for local testing
except:
    config.load_incluster_config()  # when running inside cluster
v1 = client.CoreV1Api()

def submit_pod(pod_name, namespace, image, command, scheduler_name=None, cpu_request="100m", memory_request="128Mi", priority=0):
    """Create a pod with the given name and scheduler."""
    # Create priority class if needed
    priority_class_name = None
    if priority > 0:
        priority_class_name = f"priority-{priority}"
        try:
            # Check if priority class exists
            client.SchedulingV1Api().read_priority_class(name=priority_class_name)
        except:
            # Create priority class if it doesn't exist
            priority_class = client.V1PriorityClass(
                metadata=client.V1ObjectMeta(name=priority_class_name),
                value=priority,
                global_default=False,
                description=f"Priority class for test pods with priority {priority}"
            )
            client.SchedulingV1Api().create_priority_class(body=priority_class)
    
    # Create pod manifest
    pod_manifest = client.V1Pod(
        metadata=client.V1ObjectMeta(
            name=pod_name,
            labels={"app": "scheduler-test", "scheduler": scheduler_name or "default"}
        ),
        spec=client.V1PodSpec(
            scheduler_name=scheduler_name,  # Use specified scheduler or default if None
            restart_policy="Never",
            priority_class_name=priority_class_name,
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
                        requests={"cpu": cpu_request, "memory": memory_request}
                    )
                )
            ]
        )
    )
    try:
        v1.create_namespaced_pod(namespace=namespace, body=pod_manifest)
    except Exception as e:
        print(f"Error creating pod {pod_name}: {e}")

def get_pod_start_time(pod_name, namespace):
    """Return the pod's start time (when container started running) if available."""
    try:
        pod = v1.read_namespaced_pod(name=pod_name, namespace=namespace)
        # pod.status.start_time is a datetime object when the pod has started
        return pod.status.start_time
    except Exception as e:
        print(f"Error reading pod {pod_name}: {e}")
        return None

def run_test(num_pods, namespace, image, command, poll_interval, timeout, scheduler_name=None, 
             cpu_request="100m", memory_request="128Mi", priority=0):
    """Run a test with the specified scheduler and pod configuration."""
    # Use a prefix to distinguish between schedulers
    prefix = "default" if scheduler_name is None else scheduler_name
    prefix = prefix.replace("-", "")  # Remove hyphens for cleaner pod names
    
    # Add a timestamp to make pod names unique for each run
    timestamp = int(time.time())
    
    submission_times = {}  # pod name -> submission timestamp
    schedule_times = {}    # pod name -> pod start (scheduling) timestamp

    print(f"Submitting {num_pods} pods using scheduler: {scheduler_name or 'default'}")
    print(f"Pod configuration: CPU={cpu_request}, Memory={memory_request}, Priority={priority}")
    
    for i in range(num_pods):
        pod_name = f"{prefix}-test-pod-{timestamp}-{i}"
        submission_times[pod_name] = datetime.datetime.now(datetime.timezone.utc)
        submit_pod(pod_name, namespace, image, command, scheduler_name, 
                  cpu_request, memory_request, priority)
        # Space out submissions slightly to simulate a burst
        time.sleep(0.1)

    print("Waiting for pods to be scheduled...")
    end_time = time.time() + timeout
    while len(schedule_times) < num_pods and time.time() < end_time:
        pod_list = v1.list_namespaced_pod(
            namespace=namespace, 
            label_selector=f"app=scheduler-test,scheduler={scheduler_name or 'default'}"
        ).items
        
        for pod in pod_list:
            name = pod.metadata.name
            if name in submission_times and name not in schedule_times:
                if pod.status.start_time is not None:
                    schedule_times[name] = pod.status.start_time
                    print(f"Pod {name} scheduled at {pod.status.start_time}")
        time.sleep(poll_interval)

    if len(schedule_times) < num_pods:
        print(f"Timeout reached. Only {len(schedule_times)} pods were scheduled out of {num_pods}.")

    return submission_times, schedule_times

def analyze_results(submission_times, schedule_times):
    """Compute scheduling latency (in seconds) for each pod and overall makespan."""
    latencies = {}
    for pod, sub_time in submission_times.items():
        if pod in schedule_times:
            sched_time = schedule_times[pod]
            # Ensure both times are timezone-aware
            if sub_time.tzinfo is None:
                sub_time = sub_time.replace(tzinfo=datetime.timezone.utc)
            if sched_time.tzinfo is None:
                sched_time = sched_time.replace(tzinfo=datetime.timezone.utc)
            
            latency = (sched_time - sub_time).total_seconds()
            latencies[pod] = latency

    if not latencies:
        raise ValueError("No pod scheduling events recorded!")
    
    # Makespan: time from first pod scheduled to last pod scheduled.
    all_sched_times = list(schedule_times.values())
    makespan = (max(all_sched_times) - min(all_sched_times)).total_seconds()
    avg_latency = sum(latencies.values()) / len(latencies)
    
    # Calculate additional metrics
    median_latency = np.median(list(latencies.values()))
    p95_latency = np.percentile(list(latencies.values()), 95)
    p99_latency = np.percentile(list(latencies.values()), 99)
    
    metrics = {
        "latencies": latencies,
        "makespan": makespan,
        "avg_latency": avg_latency,
        "median_latency": median_latency,
        "p95_latency": p95_latency,
        "p99_latency": p99_latency,
        "scheduled_count": len(latencies),
        "submission_count": len(submission_times)
    }
    
    return metrics

def plot_comparison(default_metrics, multi_metrics):
    """Plot comparison between default and preemptive schedulers."""
    # Create DataFrames from latencies
    default_df = pd.DataFrame(list(default_metrics["latencies"].items()), columns=["Pod", "Latency"])
    default_df["Scheduler"] = "Default Scheduler"
    
    multi_df = pd.DataFrame(list(multi_metrics["latencies"].items()), columns=["Pod", "Latency"])
    multi_df["Scheduler"] = "Preemptive Scheduler"
    
    # Combine data
    combined_df = pd.concat([default_df, multi_df])
    
    # 1. Histogram of scheduling latency
    plt.figure(figsize=(12, 6))
    plt.hist([default_df["Latency"], multi_df["Latency"]], bins=20, 
             label=["Default Scheduler", "Preemptive Scheduler"],
             alpha=0.7, edgecolor='black')
    plt.xlabel("Scheduling Latency (seconds)")
    plt.ylabel("Number of Pods")
    plt.title("Scheduling Latency Distribution Comparison")
    plt.legend()
    plt.grid(True)
    plt.savefig("latency_histogram_comparison.png")
    plt.show()
    
    # 2. Box plot of latencies
    plt.figure(figsize=(10, 6))
    sns_plot = combined_df.boxplot(column="Latency", by="Scheduler", grid=True, figsize=(10, 6))
    plt.title("Scheduling Latency Comparison")
    plt.suptitle("")  # Remove default title
    plt.ylabel("Latency (seconds)")
    plt.savefig("latency_boxplot_comparison.png")
    plt.show()
    
    # 3. Bar chart of key metrics
    metrics = ["avg_latency", "median_latency", "p95_latency", "p99_latency", "makespan"]
    default_values = [default_metrics[m] for m in metrics]
    multi_values = [multi_metrics[m] for m in metrics]
    
    x = np.arange(len(metrics))
    width = 0.35
    
    fig, ax = plt.subplots(figsize=(12, 6))
    ax.bar(x - width/2, default_values, width, label='Default Scheduler')
    ax.bar(x + width/2, multi_values, width, label='Preemptive Scheduler')
    
    ax.set_ylabel('Seconds')
    ax.set_title('Scheduler Performance Metrics Comparison')
    ax.set_xticks(x)
    ax.set_xticklabels(['Avg Latency', 'Median Latency', 'P95 Latency', 'P99 Latency', 'Makespan'])
    ax.legend()
    
    # Add values on top of bars
    for i, v in enumerate(default_values):
        ax.text(i - width/2, v + 0.1, f"{v:.2f}s", ha='center')
    for i, v in enumerate(multi_values):
        ax.text(i + width/2, v + 0.1, f"{v:.2f}s", ha='center')
    
    plt.savefig("metrics_comparison.png")
    plt.show()
    
    # Print summary
    print("\n===== PERFORMANCE COMPARISON =====")
    print(f"{'Metric':<20} {'Default Scheduler':<20} {'Preemptive Scheduler':<20} {'Improvement':<15}")
    print("-" * 75)
    
    for metric in metrics:
        default_val = default_metrics[metric]
        multi_val = multi_metrics[metric]
        if default_val == 0:
            # Handle division by zero
            improvement = 0 if multi_val == 0 else float('inf') if multi_val < 0 else float('-inf')
        else:
            improvement = ((default_val - multi_val) / default_val) * 100
        print(f"{metric:<20} {default_val:<20.2f} {multi_val:<20.2f} {improvement:<15.2f}%")
    
    print(f"{'Scheduled Pods':<20} {default_metrics['scheduled_count']:<20} {multi_metrics['scheduled_count']:<20}")
    
    # Calculate overall performance score (lower is better)
    default_score = default_metrics["avg_latency"] * 0.4 + default_metrics["p95_latency"] * 0.3 + default_metrics["makespan"] * 0.3
    multi_score = multi_metrics["avg_latency"] * 0.4 + multi_metrics["p95_latency"] * 0.3 + multi_metrics["makespan"] * 0.3
    
    score_improvement = ((default_score - multi_score) / default_score) * 100
    print(f"{'Overall Score':<20} {default_score:<20.2f} {multi_score:<20.2f} {score_improvement:<15.2f}%")
    
    return combined_df

def plot_single_results(metrics, scheduler_name):
    """Plot results for a single scheduler."""
    # Create DataFrame from latencies
    df = pd.DataFrame(list(metrics["latencies"].items()), columns=["Pod", "Latency"])
    
    # Histogram of scheduling latency
    plt.figure(figsize=(10, 5))
    plt.hist(df["Latency"], bins=20, alpha=0.75, edgecolor='black')
    plt.xlabel("Scheduling Latency (seconds)")
    plt.ylabel("Number of Pods")
    plt.title(f"Scheduling Latency Distribution - {scheduler_name}")
    plt.grid(True)
    plt.savefig(f"{scheduler_name.lower().replace(' ', '_')}_latency_histogram.png")
    plt.show()
    
    # Get schedule times
    schedule_times = {pod: metrics["submission_times"][pod] + datetime.timedelta(seconds=latency) 
                     for pod, latency in metrics["latencies"].items()}
    
    # Plot cumulative number of pods scheduled vs time
    sorted_times = sorted([t for t in schedule_times.values()])
    start_time = min(sorted_times)
    cumulative = [(t - start_time).total_seconds() for t in sorted_times]
    cum_counts = list(range(1, len(sorted_times) + 1))
    
    plt.figure(figsize=(10, 5))
    plt.plot(cumulative, cum_counts, marker='o', linestyle='-')
    plt.xlabel("Time since first pod scheduled (seconds)")
    plt.ylabel("Cumulative number of pods scheduled")
    plt.title(f"Cumulative Scheduling Throughput - {scheduler_name}")
    plt.grid(True)
    plt.savefig(f"{scheduler_name.lower().replace(' ', '_')}_cumulative.png")
    plt.show()
    
    # Print summary
    print(f"\n===== {scheduler_name.upper()} PERFORMANCE =====")
    print(f"Average scheduling latency: {metrics['avg_latency']:.2f} seconds")
    print(f"Median scheduling latency: {metrics['median_latency']:.2f} seconds")
    print(f"95th percentile latency: {metrics['p95_latency']:.2f} seconds")
    print(f"99th percentile latency: {metrics['p99_latency']:.2f} seconds")
    print(f"Overall makespan: {metrics['makespan']:.2f} seconds")
    print(f"Scheduled pods: {metrics['scheduled_count']} out of {metrics['submission_count']}")

def cleanup_pods(namespace, label_selector="app=scheduler-test"):
    """Delete all test pods."""
    print(f"Cleaning up test pods with label: {label_selector}")
    try:
        v1.delete_collection_namespaced_pod(namespace=namespace, label_selector=label_selector)
        print("Cleanup completed.")
    except Exception as e:
        print(f"Error during cleanup: {e}")

def save_results(results, filename):
    """Save test results to a file."""
    # Convert datetime objects to strings
    for scheduler, metrics in results.items():
        if "submission_times" in metrics:
            metrics["submission_times"] = {pod: str(dt) for pod, dt in metrics["submission_times"].items()}
        if "schedule_times" in metrics:
            metrics["schedule_times"] = {pod: str(dt) for pod, dt in metrics["schedule_times"].items()}
    
    with open(filename, 'w') as f:
        json.dump(results, f, indent=2)
    print(f"Results saved to {filename}")

def load_results(filename):
    """Load test results from a file."""
    with open(filename, 'r') as f:
        results = json.load(f)
    
    # Convert string timestamps back to datetime objects
    for scheduler, metrics in results.items():
        if "submission_times" in metrics:
            metrics["submission_times"] = {pod: datetime.datetime.fromisoformat(dt) 
                                         for pod, dt in metrics["submission_times"].items()}
        if "schedule_times" in metrics:
            metrics["schedule_times"] = {pod: datetime.datetime.fromisoformat(dt) 
                                       for pod, dt in metrics["schedule_times"].items()}
    
    return results

if __name__ == "__main__":
    # Try to import seaborn for prettier plots
    try:
        import seaborn as sns
        sns.set_theme(style="whitegrid")
    except ImportError:
        print("Seaborn not installed. Using matplotlib defaults.")
    
    results = {}
    
    if args.load_results:
        # Load previous results instead of running new tests
        if os.path.exists(args.output_file):
            results = load_results(args.output_file)
            print(f"Loaded results from {args.output_file}")
            
            if "default" in results and "preemptive" in results:
                plot_comparison(results["default"], results["preemptive"])
            elif args.scheduler in results:
                plot_single_results(results[args.scheduler], 
                                   "Default Scheduler" if args.scheduler == "default" else "Preemptive Scheduler")
            else:
                print(f"No results found for scheduler: {args.scheduler}")
        else:
            print(f"Results file {args.output_file} not found.")
        exit(0)
    
    # Clean up any existing test pods
    cleanup_pods(args.namespace)
    
    if args.compare:
        # Run tests for both schedulers
        
        # 1. Default scheduler test
        print("\n===== TESTING DEFAULT SCHEDULER =====")
        default_submission_times, default_schedule_times = run_test(
            num_pods=args.num_pods,
            namespace=args.namespace,
            image=args.image,
            command=args.command,
            poll_interval=args.poll_interval,
            timeout=args.timeout,
            scheduler_name=None,  # Use default scheduler
            cpu_request=args.cpu_request,
            memory_request=args.memory_request,
            priority=args.priority
        )
        
        default_metrics = analyze_results(default_submission_times, default_schedule_times)
        default_metrics["submission_times"] = default_submission_times
        default_metrics["schedule_times"] = default_schedule_times
        results["default"] = default_metrics
        
        # Clean up before next test
        cleanup_pods(args.namespace, "app=scheduler-test,scheduler=default")
        time.sleep(10)  # Wait for cleanup to complete
        
        # 2. Multi-resource scheduler test
        print("\n===== TESTING PREEMPTIVE SCHEDULER =====")
        multi_submission_times, multi_schedule_times = run_test(
            num_pods=args.num_pods,
            namespace=args.namespace,
            image=args.image,
            command=args.command,
            poll_interval=args.poll_interval,
            timeout=args.timeout,
            scheduler_name="preemptive-scheduler",  # Use the preemptive scheduler
            cpu_request=args.cpu_request,
            memory_request=args.memory_request,
            priority=args.priority
        )
        
        multi_metrics = analyze_results(multi_submission_times, multi_schedule_times)
        multi_metrics["submission_times"] = multi_submission_times
        multi_metrics["schedule_times"] = multi_schedule_times
        results["preemptive"] = multi_metrics
        
        # Plot comparison
        plot_comparison(default_metrics, multi_metrics)
        
    else:
        # Run test for single scheduler
        scheduler_name = "preemptive-scheduler" if args.scheduler == "preemptive" else None
        display_name = "Preemptive Scheduler" if args.scheduler == "preemptive" else "Default Scheduler"
        
        print(f"\n===== TESTING {display_name.upper()} =====")
        submission_times, schedule_times = run_test(
            num_pods=args.num_pods,
            namespace=args.namespace,
            image=args.image,
            command=args.command,
            poll_interval=args.poll_interval,
            timeout=args.timeout,
            scheduler_name=scheduler_name,
            cpu_request=args.cpu_request,
            memory_request=args.memory_request,
            priority=args.priority
        )
        
        metrics = analyze_results(submission_times, schedule_times)
        metrics["submission_times"] = submission_times
        metrics["schedule_times"] = schedule_times
        results[args.scheduler] = metrics
        
        # Plot single results
        plot_single_results(metrics, display_name)
    
    # Save results
    save_results(results, args.output_file)
    
    # Final cleanup
    cleanup_pods(args.namespace)
