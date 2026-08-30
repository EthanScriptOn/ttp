package runtime

import (
	"context"
	"errors"
	"time"
)

var (
	ErrClusterNotFound     = errors.New("runtime cluster not found")
	ErrProjectNotFound     = errors.New("runtime project not found")
	ErrPodNotFound         = errors.New("runtime pod not found")
	ErrContainerNotFound   = errors.New("runtime container not found")
	ErrInvalidRuntimeInput = errors.New("invalid runtime input")
)

type PodPhase string

const (
	PodPending   PodPhase = "Pending"
	PodRunning   PodPhase = "Running"
	PodSucceeded PodPhase = "Succeeded"
	PodFailed    PodPhase = "Failed"
)

type PodRef struct {
	ClusterID string `json:"cluster_id"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// ProjectID is an optional server-side scope. It is omitted from JSON so
	// existing runtime clients remain compatible, while project APIs can ensure
	// a pod operation cannot cross project boundaries.
	ProjectID string `json:"-"`
}

type ContainerStatus struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Ready        bool   `json:"ready"`
	RestartCount int    `json:"restart_count"`
}

type Pod struct {
	PodRef
	ProjectID        string            `json:"project_id"`
	NodeName         string            `json:"node_name"`
	PodIP            string            `json:"pod_ip"`
	Phase            PodPhase          `json:"phase"`
	Ready            bool              `json:"ready"`
	RestartCount     int               `json:"restart_count"`
	CPUUsageMilli    int64             `json:"cpu_millicores"`
	MemoryUsageBytes int64             `json:"memory_bytes"`
	MetricsAvailable bool              `json:"metrics_available"`
	MetricsSource    string            `json:"metrics_source,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	StartedAt        time.Time         `json:"started_at"`
}

type PodDetail struct {
	Pod
	Containers  map[string]ContainerStatus `json:"containers"`
	Config      map[string]string          `json:"config"`
	Environment map[string]string          `json:"environment"`
}

type PodLogRequest struct {
	PodRef
	Container string
	TailLines int
}

type PodConfigUpdate struct {
	Config      map[string]string `json:"config"`
	Environment map[string]string `json:"environment"`
}

// ReleaseDeployment is the small runtime contract used by the release
// executor. A provider may implement it to apply a built image; providers
// that only observe a cluster can omit it and remain read-only.
type ReleaseDeployment struct {
	ClusterID        string
	Namespace        string
	ProjectID        string
	ReleaseID        string
	Branch           string
	CommitSHA        string
	Image            string
	Replicas         int
	Strategy         string
	StablePercent    int
	CandidatePercent int
	BluePercent      int
	GreenPercent     int
	Manifest         string
	ManifestFormat   string
	ManifestVersion  int
}

// ReleaseDeployer is intentionally optional. It keeps the current
// observation-only Kubernetes provider safe while allowing the local demo
// provider to show the full commit-to-Pod experience.
type ReleaseDeployer interface {
	DeployRelease(ctx context.Context, deployment ReleaseDeployment) error
}

// ClusterConnection is the safe result of a connection check. Authentication
// material is intentionally never part of this response.
type ClusterConnection struct {
	Version string `json:"version,omitempty"`
}

// ClusterRegistrar is implemented by providers that can add a cluster while
// the control plane is running. The kubeconfig path is read on the server and
// its contents stay inside the provider/client-go process.
type ClusterRegistrar interface {
	RegisterKubeconfigWithContext(clusterID, kubeconfigPath, kubeContext string) error
	RegisterInCluster(clusterID string) error
}

type ClusterChecker interface {
	CheckCluster(ctx context.Context, clusterID string) (ClusterConnection, error)
}

// MetricPoint is one timestamped sample returned by a runtime provider. The
// demo provider fills these values so the UI can be exercised before a
// Prometheus data source is connected; Kubernetes providers may leave the
// series empty when historical metrics are unavailable.
type MetricPoint struct {
	Timestamp           time.Time `json:"timestamp"`
	CPUUsedPercent      float64   `json:"cpu_used_percent"`
	MemoryUsedPercent   float64   `json:"memory_used_percent"`
	SwapUsedPercent     float64   `json:"swap_used_percent"`
	DiskUsedPercent     float64   `json:"disk_used_percent"`
	DiskReadMbps        float64   `json:"disk_read_mbps"`
	DiskWriteMbps       float64   `json:"disk_write_mbps"`
	NetworkReceiveMbps  float64   `json:"network_receive_mbps"`
	NetworkTransmitMbps float64   `json:"network_transmit_mbps"`
	Load1               float64   `json:"load_1m"`
	Load5               float64   `json:"load_5m"`
	Load15              float64   `json:"load_15m"`
	RequestRateRPS      float64   `json:"request_rate_rps"`
	ErrorRatePercent    float64   `json:"error_rate_percent"`
	LatencyP50Ms        float64   `json:"latency_p50_ms"`
	LatencyP95Ms        float64   `json:"latency_p95_ms"`
	LatencyP99Ms        float64   `json:"latency_p99_ms"`
	PodCount            int       `json:"pod_count"`
	HealthyPodCount     int       `json:"healthy_pod_count"`
	PodRestartCount     int       `json:"pod_restart_count"`
}

// NodeMetrics contains the current node-level view used by the cluster
// dashboard. Resource values are only populated by a provider with a metrics
// source; node identity and readiness can still be reported by Kubernetes API.
type NodeMetrics struct {
	Name                string  `json:"name"`
	Ready               bool    `json:"ready"`
	CPUUsedPercent      float64 `json:"cpu_used_percent"`
	MemoryUsedPercent   float64 `json:"memory_used_percent"`
	SwapUsedPercent     float64 `json:"swap_used_percent"`
	DiskUsedPercent     float64 `json:"disk_used_percent"`
	Load1               float64 `json:"load_1m"`
	NetworkReceiveMbps  float64 `json:"network_receive_mbps"`
	NetworkTransmitMbps float64 `json:"network_transmit_mbps"`
	PodCount            int     `json:"pod_count"`
}

type ClusterMetrics struct {
	ClusterID           string        `json:"cluster_id"`
	CPUUsedPercent      float64       `json:"cpu_used_percent"`
	MemoryUsedPercent   float64       `json:"memory_used_percent"`
	SwapUsedPercent     float64       `json:"swap_used_percent"`
	DiskUsedPercent     float64       `json:"disk_used_percent"`
	DiskReadMbps        float64       `json:"disk_read_mbps"`
	DiskWriteMbps       float64       `json:"disk_write_mbps"`
	NetworkReceiveMbps  float64       `json:"network_receive_mbps"`
	NetworkTransmitMbps float64       `json:"network_transmit_mbps"`
	Load1               float64       `json:"load_1m"`
	Load5               float64       `json:"load_5m"`
	Load15              float64       `json:"load_15m"`
	RequestRateRPS      float64       `json:"request_rate_rps"`
	ErrorRatePercent    float64       `json:"error_rate_percent"`
	LatencyP50Ms        float64       `json:"latency_p50_ms"`
	LatencyP95Ms        float64       `json:"latency_p95_ms"`
	LatencyP99Ms        float64       `json:"latency_p99_ms"`
	PodRestartCount     int           `json:"pod_restart_count"`
	PendingPodCount     int           `json:"pending_pod_count"`
	FailedPodCount      int           `json:"failed_pod_count"`
	CrashLoopCount      int           `json:"crash_loop_count"`
	OOMKilledCount      int           `json:"oom_killed_count"`
	NodeCount           int           `json:"node_count"`
	ReadyNodeCount      int           `json:"ready_node_count"`
	DeploymentDesired   int           `json:"deployment_desired"`
	DeploymentAvailable int           `json:"deployment_available"`
	MetricsSource       string        `json:"metrics_source,omitempty"`
	MetricsAvailable    bool          `json:"metrics_available"`
	MetricsMessage      string        `json:"metrics_message,omitempty"`
	PodCount            int           `json:"pod_count"`
	HealthyPodCount     int           `json:"healthy_pod_count"`
	ObservedAt          time.Time     `json:"observed_at"`
	Series              []MetricPoint `json:"series,omitempty"`
	Nodes               []NodeMetrics `json:"nodes,omitempty"`
}

type ProjectMetrics struct {
	ClusterID           string        `json:"cluster_id"`
	ProjectID           string        `json:"project_id"`
	PodCount            int           `json:"pod_count"`
	HealthyPodCount     int           `json:"healthy_pod_count"`
	CPUUsedPercent      float64       `json:"cpu_used_percent"`
	MemoryUsedPercent   float64       `json:"memory_used_percent"`
	SwapUsedPercent     float64       `json:"swap_used_percent"`
	DiskUsedPercent     float64       `json:"disk_used_percent"`
	DiskReadMbps        float64       `json:"disk_read_mbps"`
	DiskWriteMbps       float64       `json:"disk_write_mbps"`
	NetworkReceiveMbps  float64       `json:"network_receive_mbps"`
	NetworkTransmitMbps float64       `json:"network_transmit_mbps"`
	Load1               float64       `json:"load_1m"`
	Load5               float64       `json:"load_5m"`
	Load15              float64       `json:"load_15m"`
	RequestRateRPS      float64       `json:"request_rate_rps"`
	ErrorRatePercent    float64       `json:"error_rate_percent"`
	LatencyP50Ms        float64       `json:"latency_p50_ms"`
	LatencyP95Ms        float64       `json:"latency_p95_ms"`
	LatencyP99Ms        float64       `json:"latency_p99_ms"`
	PodRestartCount     int           `json:"pod_restart_count"`
	PendingPodCount     int           `json:"pending_pod_count"`
	FailedPodCount      int           `json:"failed_pod_count"`
	CrashLoopCount      int           `json:"crash_loop_count"`
	OOMKilledCount      int           `json:"oom_killed_count"`
	NodeCount           int           `json:"node_count"`
	ReadyNodeCount      int           `json:"ready_node_count"`
	DeploymentDesired   int           `json:"deployment_desired"`
	DeploymentAvailable int           `json:"deployment_available"`
	MetricsSource       string        `json:"metrics_source,omitempty"`
	MetricsAvailable    bool          `json:"metrics_available"`
	MetricsMessage      string        `json:"metrics_message,omitempty"`
	ObservedAt          time.Time     `json:"observed_at"`
	Series              []MetricPoint `json:"series,omitempty"`
	Nodes               []NodeMetrics `json:"nodes,omitempty"`
}

// Provider is the runtime boundary. A client-go implementation can map these
// operations to typed Kubernetes clients without changing the HTTP contract.
type Provider interface {
	ListPods(ctx context.Context, clusterID, projectID string) ([]Pod, error)
	GetPod(ctx context.Context, ref PodRef) (PodDetail, error)
	GetPodLogs(ctx context.Context, request PodLogRequest) (string, error)
	UpdatePodConfig(ctx context.Context, ref PodRef, update PodConfigUpdate) (PodDetail, error)
	GetClusterMetrics(ctx context.Context, clusterID string) (ClusterMetrics, error)
	GetProjectMetrics(ctx context.Context, clusterID, projectID string) (ProjectMetrics, error)
}
