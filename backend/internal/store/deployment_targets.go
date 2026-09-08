package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
)

const (
	defaultTargetEnvironment = "dev"
	defaultTargetName        = "开发环境"
	defaultTargetNamespace   = "lab"
)

const (
	DeploymentStageDev    = "dev"
	DeploymentStageUAT    = "uat"
	DeploymentStagePre    = "pre"
	DeploymentStageProd   = "prod"
	DeploymentStageCustom = "custom"
)

func normalizeDeploymentStage(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return DeploymentStageCustom
	}
	switch value {
	case DeploymentStageDev, DeploymentStageUAT, DeploymentStagePre, DeploymentStageProd, DeploymentStageCustom:
		return value
	default:
		return ""
	}
}

func validateDeploymentStageOrder(stage string, sortOrder int) error {
	stage = normalizeDeploymentStage(stage)
	if stage == "" {
		return fmt.Errorf("%w: stage is invalid", ErrInvalidInput)
	}
	if sortOrder < 1 {
		return fmt.Errorf("%w: sort order must be at least 1", ErrInvalidInput)
	}
	if sortOrder == 1 && stage != DeploymentStageDev {
		return fmt.Errorf("%w: sort order 1 must be the DEV stage", ErrInvalidInput)
	}
	if stage == DeploymentStageDev && sortOrder != 1 {
		return fmt.Errorf("%w: DEV stage must have sort order 1", ErrInvalidInput)
	}
	return nil
}

func legacyDeploymentStage(environment string) string {
	switch strings.TrimSpace(strings.ToLower(environment)) {
	case "dev", "develop", "development":
		return DeploymentStageDev
	case "uat", "test", "testing":
		return DeploymentStageUAT
	case "pre", "preprod", "pre-production":
		return DeploymentStagePre
	case "prod", "production":
		return DeploymentStageProd
	default:
		return DeploymentStageCustom
	}
}

func newDeploymentTarget(spaceID, projectID string, input CreateDeploymentTargetInput, fallback domain.Project) (domain.DeploymentTarget, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = defaultTargetName
	}
	if len([]rune(name)) > 120 {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: target name is too long", ErrInvalidInput)
	}
	environment := strings.TrimSpace(input.Environment)
	if environment == "" {
		environment = defaultTargetEnvironment
	}
	if len([]rune(environment)) > 64 || strings.ContainsAny(environment, "\x00\r\n") {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: target environment is invalid", ErrInvalidInput)
	}
	stage := normalizeDeploymentStage(input.Stage)
	if stage == "" {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: stage is invalid", ErrInvalidInput)
	}
	sortOrder := input.SortOrder
	if sortOrder == 0 {
		sortOrder = 1
	}
	if err := validateDeploymentStageOrder(stage, sortOrder); err != nil {
		return domain.DeploymentTarget{}, err
	}
	clusterID := strings.TrimSpace(input.ClusterID)
	if clusterID == "" {
		clusterID = fallback.ClusterID
	}
	clusterID = normalizeClusterID(spaceID, clusterID)
	namespace := strings.TrimSpace(input.Namespace)
	if namespace == "" {
		namespace = strings.TrimSpace(fallback.Namespace)
	}
	if namespace == "" {
		namespace = defaultTargetNamespace
	}
	if !validDeploymentNamespace(namespace) {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: namespace is invalid", ErrInvalidInput)
	}
	strategy := strings.TrimSpace(strings.ToLower(input.DeployStrategy))
	if strategy == "" {
		strategy = strings.TrimSpace(strings.ToLower(fallback.DeployStrategy))
	}
	if strategy == "" {
		strategy = "rolling"
	}
	if strategy != "rolling" && strategy != "canary" && strategy != "blue_green" {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: deploy strategy is invalid", ErrInvalidInput)
	}
	replicas := input.Replicas
	if replicas == 0 {
		replicas = fallback.Replicas
	}
	if replicas <= 0 || replicas > 100 {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: replicas must be between 1 and 100", ErrInvalidInput)
	}
	port := input.ContainerPort
	if port == 0 {
		port = fallback.ContainerPort
	}
	if port <= 0 || port > 65535 {
		return domain.DeploymentTarget{}, fmt.Errorf("%w: container port must be between 1 and 65535", ErrInvalidInput)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	return domain.DeploymentTarget{
		ID:             "target-" + uuid.NewString(),
		ProjectID:      projectID,
		SpaceID:        spaceID,
		Name:           name,
		Environment:    environment,
		Stage:          stage,
		SortOrder:      sortOrder,
		ClusterID:      clusterID,
		Namespace:      namespace,
		Replicas:       replicas,
		ContainerPort:  port,
		DeployStrategy: strategy,
		Enabled:        enabled,
		Status:         "active",
		Health:         "unknown",
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func updateDeploymentTarget(current domain.DeploymentTarget, input UpdateDeploymentTargetInput) (domain.DeploymentTarget, error) {
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 120 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: target name is invalid", ErrInvalidInput)
		}
		current.Name = name
	}
	if input.Environment != nil {
		environment := strings.TrimSpace(*input.Environment)
		if environment == "" || len([]rune(environment)) > 64 || strings.ContainsAny(environment, "\x00\r\n") {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: target environment is invalid", ErrInvalidInput)
		}
		current.Environment = environment
	}
	if input.Stage != nil {
		stage := normalizeDeploymentStage(*input.Stage)
		if stage == "" {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: stage is invalid", ErrInvalidInput)
		}
		current.Stage = stage
	}
	if input.SortOrder != nil {
		if *input.SortOrder < 1 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: sort order must be at least 1", ErrInvalidInput)
		}
		current.SortOrder = *input.SortOrder
	}
	if err := validateDeploymentStageOrder(current.Stage, current.SortOrder); err != nil {
		return domain.DeploymentTarget{}, err
	}
	if input.ClusterID != nil {
		clusterID := strings.TrimSpace(*input.ClusterID)
		if clusterID == "" {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: cluster is required", ErrInvalidInput)
		}
		current.ClusterID = clusterID
	}
	if input.Namespace != nil {
		namespace := strings.TrimSpace(*input.Namespace)
		if !validDeploymentNamespace(namespace) {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: namespace is invalid", ErrInvalidInput)
		}
		current.Namespace = namespace
	}
	if input.Replicas != nil {
		if *input.Replicas <= 0 || *input.Replicas > 100 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: replicas must be between 1 and 100", ErrInvalidInput)
		}
		current.Replicas = *input.Replicas
	}
	if input.ContainerPort != nil {
		if *input.ContainerPort <= 0 || *input.ContainerPort > 65535 {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: container port must be between 1 and 65535", ErrInvalidInput)
		}
		current.ContainerPort = *input.ContainerPort
	}
	if input.DeployStrategy != nil {
		strategy := strings.TrimSpace(strings.ToLower(*input.DeployStrategy))
		if strategy != "rolling" && strategy != "canary" && strategy != "blue_green" {
			return domain.DeploymentTarget{}, fmt.Errorf("%w: deploy strategy is invalid", ErrInvalidInput)
		}
		current.DeployStrategy = strategy
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	current.UpdatedAt = time.Now().UTC()
	return current, nil
}

func validDeploymentNamespace(value string) bool {
	if len(value) == 0 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
}
