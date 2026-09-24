package runtime

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

var (
	ErrClusterNotFound                = errors.New("runtime cluster not found")
	ErrProjectNotFound                = errors.New("runtime project not found")
	ErrPodNotFound                    = errors.New("runtime pod not found")
	ErrContainerNotFound              = errors.New("runtime container not found")
	ErrInvalidRuntimeInput            = errors.New("invalid runtime input")
	ErrPodExecUnsupported             = errors.New("runtime provider does not support pod exec")
	ErrEnvironmentCleanupUnsupported  = errors.New("runtime provider does not support environment cleanup")
	ErrEnvironmentCleanupFailed       = errors.New("runtime environment cleanup failed")
	ErrReleaseAccessDenied            = errors.New("runtime release permissions are insufficient")
	ErrReleaseTrafficUnsupported      = errors.New("runtime provider does not support release traffic adjustment")
	ErrRegistryPullTestUnsupported    = errors.New("runtime provider does not support registry pull testing")
	ErrNamespaceManagementUnsupported = errors.New("runtime provider does not support namespace management")
	ErrNamespaceOwnershipConflict     = errors.New("runtime namespace is owned by another resource")
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
	ProjectID    string            `json:"project_id"`
	NodeName     string            `json:"node_name"`
	PodIP        string            `json:"pod_ip"`
	Phase        PodPhase          `json:"phase"`
	Ready        bool              `json:"ready"`
	RestartCount int               `json:"restart_count"`
	Labels       map[string]string `json:"labels,omitempty"`
	StartedAt    time.Time         `json:"started_at"`
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

type PodExecRequest struct {
	PodRef
	Container string `json:"container"`
	Command   string `json:"command"`
}

type PodExecResult struct {
	Output string `json:"output"`
}

// PodTerminalRequest identifies an interactive shell session. Unlike
// PodExecRequest, the command is intentionally not supplied by the caller:
// the runtime starts a login-compatible /bin/sh attached to a TTY and keeps it
// alive until the browser closes the terminal.
type PodTerminalRequest struct {
	PodRef
	Container string `json:"container"`
}

// TerminalSize is the browser terminal viewport in character cells.
type TerminalSize struct {
	Columns uint16 `json:"cols"`
	Rows    uint16 `json:"rows"`
}

// PodTerminalStream is optional so observation-only providers remain
// compatible. Implementations must enforce the PodRef project scope before
// attaching a PTY.
type PodTerminalStream interface {
	StreamPodTerminal(ctx context.Context, request PodTerminalRequest, stdin io.Reader, stdout, stderr io.Writer, sizes <-chan TerminalSize) error
}

// ReleaseLogFunc receives one line of executor output. It is deliberately a
// callback rather than part of the persisted deployment payload so providers
// can stream their native API/command output without changing the runtime
// contract used by existing callers.
type ReleaseLogFunc func(source, stream, level, line string)

// ReleaseDeployment is the small runtime contract used by the release
// executor. A provider may implement it to apply a built image; providers
// that only observe a cluster can omit it and remain read-only.
type ReleaseDeployment struct {
	ClusterID        string
	Namespace        string
	ProjectID        string
	TargetID         string
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
	// ResourceFiles contains one independently applicable Kubernetes object per
	// entry. Each entry is validated and applied separately.
	ResourceFiles       []string
	ImagePullCredential *ImagePullCredential
	Log                 ReleaseLogFunc `json:"-"`
}

// ImagePullCredential is short-lived release input. KubernetesProvider turns
// it into a namespaced dockerconfigjson Secret and never logs the secret.
type ImagePullCredential struct {
	ConnectionID string
	Registry     string
	AuthType     string
	Username     string
	Secret       string
	SecretName   string
}

// ReleaseDeployer is intentionally optional. It keeps the current
// observation-only Kubernetes provider safe while allowing an explicitly
// injected release provider to be tested.
type ReleaseDeployer interface {
	DeployRelease(ctx context.Context, deployment ReleaseDeployment) error
}

// ReleaseTrafficUpdate describes a live traffic split change for one release
// target. Providers may use it to update the runtime router or workload
// metadata without rebuilding the release artifact.
type ReleaseTrafficUpdate struct {
	ClusterID        string
	Namespace        string
	ProjectID        string
	ReleaseID        string
	TargetID         string
	Strategy         string
	StablePercent    int
	CandidatePercent int
	BluePercent      int
	GreenPercent     int
}

type ReleaseTrafficUpdater interface {
	UpdateReleaseTraffic(ctx context.Context, update ReleaseTrafficUpdate) error
}

// EnvironmentCleaner is required before a deployment target can be removed.
// Implementations must delete only resources owned by the supplied project and
// target, then return after the Kubernetes API confirms they are gone.
type EnvironmentCleaner interface {
	CleanupEnvironment(ctx context.Context, clusterID, namespace, projectID, targetID string) error
}

// ABExperimentDeployment creates two independently labelled workloads in one
// environment. It is optional because a provider must understand how to
// route traffic before it can safely run an A/B experiment.
type ABExperimentDeployment struct {
	ClusterID    string
	Namespace    string
	ProjectID    string
	ExperimentID string
	ABranch      string
	ACommitSHA   string
	AReleaseID   string
	BBranch      string
	BCommitSHA   string
	BReleaseID   string
	AImage       string
	BImage       string
	Replicas     int
	Strategy     string
	Assignment   string
	RoutingRule  domain.ABRoutingRule
	ATraffic     int
	BTraffic     int
	Log          ReleaseLogFunc `json:"-"`
}

type ABExperimentDeployer interface {
	DeployABExperiment(ctx context.Context, deployment ABExperimentDeployment) error
	UpdateABExperimentTraffic(ctx context.Context, clusterID, namespace, projectID, experimentID string, aTraffic, bTraffic int) error
	StopABExperiment(ctx context.Context, clusterID, namespace, projectID, experimentID string, keepVariant string) error
	ListABExperimentPods(ctx context.Context, clusterID, namespace, projectID, experimentID string) ([]Pod, error)
}

type ABVariantMetrics struct {
	MetricsAvailable bool
	MetricsMessage   string
	RequestRateRPS   float64
	ErrorRatePercent float64
	LatencyP95MS     float64
}

type ABExperimentMetrics struct {
	A ABVariantMetrics
	B ABVariantMetrics
}

// ABExperimentMetricsProvider is optional because a real Kubernetes runtime
// needs a metrics backend, such as Prometheus, in addition to the API client.
type ABExperimentMetricsProvider interface {
	GetABExperimentMetrics(ctx context.Context, clusterID, namespace, projectID, experimentID string) (ABExperimentMetrics, error)
}

// ABExperimentCleaner is used when setup fails after the experiment record has
// been created. It removes every workload belonging to that experiment.
type ABExperimentCleaner interface {
	CleanupABExperiment(ctx context.Context, clusterID, namespace, projectID, experimentID string) error
}

// ClusterConnection is the safe result of a connection check. Authentication
// material is intentionally never part of this response.
type ClusterConnection struct {
	Version string `json:"version,omitempty"`
}

type MonitoringStatus struct {
	State            string                 `json:"state,omitempty"`
	Available        bool                   `json:"available"`
	Component        string                 `json:"component,omitempty"`
	DisplayName      string                 `json:"display_name,omitempty"`
	Installable      bool                   `json:"installable"`
	Message          string                 `json:"message,omitempty"`
	Installed        bool                   `json:"installed"`
	InstallVersion   string                 `json:"install_version,omitempty"`
	Dependencies     []MonitoringDependency `json:"dependencies,omitempty"`
	HistoryAvailable bool                   `json:"history_available"`
	RetentionDays    int                    `json:"retention_days,omitempty"`
}

type MonitoringDependency struct {
	Component      string `json:"component"`
	DisplayName    string `json:"display_name"`
	State          string `json:"state,omitempty"`
	Available      bool   `json:"available"`
	Installed      bool   `json:"installed"`
	Installable    bool   `json:"installable"`
	InstallVersion string `json:"install_version,omitempty"`
	Message        string `json:"message,omitempty"`
	ImageSource    string `json:"image_source,omitempty"`
	SourceIndex    int    `json:"source_index,omitempty"`
	SourceCount    int    `json:"source_count,omitempty"`
	RetryCount     int    `json:"retry_count,omitempty"`
	RetryLimit     int    `json:"retry_limit,omitempty"`
}

type MonitoringInstaller interface {
	CheckMonitoring(ctx context.Context, clusterID string) (MonitoringStatus, error)
	InstallMonitoring(ctx context.Context, clusterID string) (MonitoringStatus, error)
}

type MonitoringRetentionInstaller interface {
	InstallMonitoringWithRetention(ctx context.Context, clusterID string, retentionDays int) (MonitoringStatus, error)
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

// ReleaseAccessChecker verifies the Kubernetes permissions required before a
// release is queued. It does not inspect image registry credentials; those
// belong to the node/service-account image pull path.
type ReleaseAccessChecker interface {
	CheckReleaseAccess(ctx context.Context, clusterID, namespace string) error
}

// RegistryPullTestRequest describes a short-lived image pull probe. The
// credential is used only to create a temporary imagePullSecret and is never
// returned to the caller.
type RegistryPullTestRequest struct {
	Image      string
	Registry   string
	AuthType   string
	Username   string
	Secret     string
	SecretName string
}

// RegistryPullTestResult is the safe result of a node-side image pull probe.
type RegistryPullTestResult struct {
	Namespace string   `json:"namespace"`
	PodName   string   `json:"pod_name"`
	NodeName  string   `json:"node_name,omitempty"`
	Image     string   `json:"image"`
	Phase     PodPhase `json:"phase"`
	Message   string   `json:"message,omitempty"`
}

// RegistryPullTester is implemented by runtimes that can create a temporary
// Pod and verify that a target cluster node can pull from an OCI registry.
type RegistryPullTester interface {
	TestRegistryPull(ctx context.Context, clusterID string, request RegistryPullTestRequest) (RegistryPullTestResult, error)
}

// NamespaceManager is an optional runtime extension used when an environment
// is created. Kubernetes implementations create the generated namespace and
// refuse to take over an existing namespace without matching TTP ownership
// labels.
type NamespaceManager interface {
	EnsureNamespace(ctx context.Context, clusterID, namespace string, labels map[string]string) error
}

// NamespaceQuotaManager is an optional extension used when an environment
// namespace is created or updated. Implementations must ensure the namespace
// ownership and apply the complete namespace budget atomically from the
// caller's point of view. The quota is shared by every project targeting the
// same space/cluster/environment tuple.
type NamespaceQuotaManager interface {
	EnsureNamespaceWithQuota(ctx context.Context, clusterID, namespace string, labels map[string]string, quota domain.NamespaceQuota) error
}

// MetricPoint is one timestamped sample returned by a runtime provider. The
// Providers may leave the series empty when a metrics data source is
// unavailable; the API must not fill missing samples with fabricated values.
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
	DiskReadMbps        float64 `json:"disk_read_mbps"`
	DiskWriteMbps       float64 `json:"disk_write_mbps"`
	Load1               float64 `json:"load_1m"`
	Load5               float64 `json:"load_5m"`
	Load15              float64 `json:"load_15m"`
	NetworkReceiveMbps  float64 `json:"network_receive_mbps"`
	NetworkTransmitMbps float64 `json:"network_transmit_mbps"`
	PodCount            int     `json:"pod_count"`
	Pods                []Pod   `json:"pods,omitempty"`
}

type ClusterMetrics struct {
	ClusterID           string            `json:"cluster_id"`
	CPUUsedPercent      float64           `json:"cpu_used_percent"`
	MemoryUsedPercent   float64           `json:"memory_used_percent"`
	SwapUsedPercent     float64           `json:"swap_used_percent"`
	DiskUsedPercent     float64           `json:"disk_used_percent"`
	DiskReadMbps        float64           `json:"disk_read_mbps"`
	DiskWriteMbps       float64           `json:"disk_write_mbps"`
	NetworkReceiveMbps  float64           `json:"network_receive_mbps"`
	NetworkTransmitMbps float64           `json:"network_transmit_mbps"`
	Load1               float64           `json:"load_1m"`
	Load5               float64           `json:"load_5m"`
	Load15              float64           `json:"load_15m"`
	RequestRateRPS      float64           `json:"request_rate_rps"`
	ErrorRatePercent    float64           `json:"error_rate_percent"`
	LatencyP50Ms        float64           `json:"latency_p50_ms"`
	LatencyP95Ms        float64           `json:"latency_p95_ms"`
	LatencyP99Ms        float64           `json:"latency_p99_ms"`
	PodRestartCount     int               `json:"pod_restart_count"`
	PendingPodCount     int               `json:"pending_pod_count"`
	FailedPodCount      int               `json:"failed_pod_count"`
	CrashLoopCount      int               `json:"crash_loop_count"`
	OOMKilledCount      int               `json:"oom_killed_count"`
	NodeCount           int               `json:"node_count"`
	ReadyNodeCount      int               `json:"ready_node_count"`
	DeploymentDesired   int               `json:"deployment_desired"`
	DeploymentAvailable int               `json:"deployment_available"`
	MetricsSource       string            `json:"metrics_source,omitempty"`
	MetricsAvailable    bool              `json:"metrics_available"`
	MetricsMessage      string            `json:"metrics_message,omitempty"`
	PodCount            int               `json:"pod_count"`
	HealthyPodCount     int               `json:"healthy_pod_count"`
	ObservedAt          time.Time         `json:"observed_at"`
	Series              []MetricPoint     `json:"series,omitempty"`
	Nodes               []NodeMetrics     `json:"nodes,omitempty"`
	Monitoring          *MonitoringStatus `json:"monitoring,omitempty"`
}

type ProjectMetrics struct {
	ClusterID           string            `json:"cluster_id"`
	ProjectID           string            `json:"project_id"`
	PodCount            int               `json:"pod_count"`
	HealthyPodCount     int               `json:"healthy_pod_count"`
	CPUUsedPercent      float64           `json:"cpu_used_percent"`
	MemoryUsedPercent   float64           `json:"memory_used_percent"`
	SwapUsedPercent     float64           `json:"swap_used_percent"`
	DiskUsedPercent     float64           `json:"disk_used_percent"`
	DiskReadMbps        float64           `json:"disk_read_mbps"`
	DiskWriteMbps       float64           `json:"disk_write_mbps"`
	NetworkReceiveMbps  float64           `json:"network_receive_mbps"`
	NetworkTransmitMbps float64           `json:"network_transmit_mbps"`
	Load1               float64           `json:"load_1m"`
	Load5               float64           `json:"load_5m"`
	Load15              float64           `json:"load_15m"`
	RequestRateRPS      float64           `json:"request_rate_rps"`
	ErrorRatePercent    float64           `json:"error_rate_percent"`
	LatencyP50Ms        float64           `json:"latency_p50_ms"`
	LatencyP95Ms        float64           `json:"latency_p95_ms"`
	LatencyP99Ms        float64           `json:"latency_p99_ms"`
	PodRestartCount     int               `json:"pod_restart_count"`
	PendingPodCount     int               `json:"pending_pod_count"`
	FailedPodCount      int               `json:"failed_pod_count"`
	CrashLoopCount      int               `json:"crash_loop_count"`
	OOMKilledCount      int               `json:"oom_killed_count"`
	NodeCount           int               `json:"node_count"`
	ReadyNodeCount      int               `json:"ready_node_count"`
	DeploymentDesired   int               `json:"deployment_desired"`
	DeploymentAvailable int               `json:"deployment_available"`
	MetricsSource       string            `json:"metrics_source,omitempty"`
	MetricsAvailable    bool              `json:"metrics_available"`
	MetricsMessage      string            `json:"metrics_message,omitempty"`
	ObservedAt          time.Time         `json:"observed_at"`
	Series              []MetricPoint     `json:"series,omitempty"`
	Nodes               []NodeMetrics     `json:"nodes,omitempty"`
	Monitoring          *MonitoringStatus `json:"monitoring,omitempty"`
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

// PodExecutor is optional so observation-only providers can remain read-only.
// Implementations must enforce the PodRef project scope before executing a
// command in a container.
type PodExecutor interface {
	ExecPodCommand(ctx context.Context, request PodExecRequest) (PodExecResult, error)
}
