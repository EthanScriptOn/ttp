package domain

import "time"

const (
	ABExperimentRunning  = "running"
	ABExperimentStopped  = "stopped"
	ABExperimentFinished = "finished"
)

type ABExperimentVersion struct {
	Role      string `json:"role"`
	ReleaseID string `json:"release_id,omitempty"`
	Branch    string `json:"branch"`
	CommitSHA string `json:"commit_sha"`
	ShortSHA  string `json:"short_sha,omitempty"`
	Message   string `json:"message,omitempty"`
}

type ABExperimentStats struct {
	MetricsAvailable bool    `json:"metrics_available"`
	MetricsMessage   string  `json:"metrics_message,omitempty"`
	RequestRateRPS   float64 `json:"request_rate_rps,omitempty"`
	ErrorRatePercent float64 `json:"error_rate_percent,omitempty"`
	LatencyP95MS     float64 `json:"latency_p95_ms,omitempty"`
}

// ABRoutingRule describes the request attribute used by the traffic router.
// The first implementation intentionally accepts only a safe JSON path so the
// gateway never evaluates user-provided code.
type ABRoutingRule struct {
	Source          string `json:"source"`
	Path            string `json:"path"`
	MissingBehavior string `json:"missing_behavior"`
	Algorithm       string `json:"algorithm"`
}

type ABExperimentPod struct {
	Name         string `json:"name"`
	Variant      string `json:"variant"`
	Version      string `json:"version"`
	NodeName     string `json:"node_name,omitempty"`
	PodIP        string `json:"pod_ip,omitempty"`
	Phase        string `json:"phase"`
	Ready        bool   `json:"ready"`
	RestartCount int    `json:"restart_count"`
}

type ABExperimentEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type ABExperiment struct {
	ID               string              `json:"id"`
	ProjectID        string              `json:"project_id"`
	Name             string              `json:"name"`
	TargetID         string              `json:"target_id"`
	Environment      string              `json:"environment"`
	EnvironmentStage string              `json:"environment_stage,omitempty"`
	ClusterID        string              `json:"cluster_id"`
	Namespace        string              `json:"namespace"`
	Replicas         int                 `json:"replicas"`
	Strategy         string              `json:"strategy"`
	Assignment       string              `json:"assignment"`
	RoutingRule      ABRoutingRule       `json:"routing_rule,omitempty"`
	AVersion         ABExperimentVersion `json:"a_version"`
	BVersion         ABExperimentVersion `json:"b_version"`
	ATraffic         int                 `json:"a_traffic"`
	BTraffic         int                 `json:"b_traffic"`
	AStats           ABExperimentStats   `json:"a_stats"`
	BStats           ABExperimentStats   `json:"b_stats"`
	APods            []ABExperimentPod   `json:"a_pods,omitempty"`
	BPods            []ABExperimentPod   `json:"b_pods,omitempty"`
	Status           string              `json:"status"`
	CreatedBy        uint64              `json:"created_by,omitempty"`
	StartedAt        time.Time           `json:"started_at"`
	FinishedAt       *time.Time          `json:"finished_at,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	Events           []ABExperimentEvent `json:"events,omitempty"`
}
