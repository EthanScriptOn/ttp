package store

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	maxProjectNameLength            = 120
	maxProjectDescriptionLength     = 255
	maxProjectBranchLength          = 120
	maxProjectRepositoryLength      = 500
	maxProjectIDLength              = 64
	maxProjectClusterIDLength       = 64
	maxProjectReplicas              = 100
	maxProjectImageRepositoryLength = 500
)

func normalizeCreateProjectInput(input CreateProjectInput) (CreateProjectInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return CreateProjectInput{}, fmt.Errorf("%w: project name is required", ErrInvalidInput)
	}
	if !validText(input.Name, maxProjectNameLength) {
		return CreateProjectInput{}, fmt.Errorf("%w: project name is invalid", ErrInvalidInput)
	}

	input.Description = strings.TrimSpace(input.Description)
	if !validText(input.Description, maxProjectDescriptionLength) {
		return CreateProjectInput{}, fmt.Errorf("%w: project description is invalid", ErrInvalidInput)
	}

	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	if err := validateRepositoryURL(input.RepositoryURL); err != nil {
		return CreateProjectInput{}, err
	}

	input.DefaultBranch = strings.TrimSpace(input.DefaultBranch)
	if input.DefaultBranch == "" {
		input.DefaultBranch = "main"
	}
	if !validGitBranch(input.DefaultBranch) {
		return CreateProjectInput{}, fmt.Errorf("%w: default branch is invalid", ErrInvalidInput)
	}

	input.ClusterID = strings.TrimSpace(input.ClusterID)
	if input.ClusterID == "" {
		return CreateProjectInput{}, fmt.Errorf("%w: cluster_id is required", ErrInvalidInput)
	}
	if len(input.ClusterID) > maxProjectClusterIDLength || hasControl(input.ClusterID) {
		return CreateProjectInput{}, fmt.Errorf("%w: cluster_id is invalid", ErrInvalidInput)
	}

	input.Namespace = strings.TrimSpace(input.Namespace)
	if input.Namespace == "" {
		input.Namespace = defaultTargetNamespace
	}
	if !validDeploymentNamespace(input.Namespace) {
		return CreateProjectInput{}, fmt.Errorf("%w: namespace is invalid", ErrInvalidInput)
	}

	input.DeployStrategy = strings.ToLower(strings.TrimSpace(input.DeployStrategy))
	if input.DeployStrategy == "" {
		input.DeployStrategy = "rolling"
	}
	if !validDeployStrategy(input.DeployStrategy) {
		return CreateProjectInput{}, fmt.Errorf("%w: deploy strategy is invalid", ErrInvalidInput)
	}

	if input.Replicas < 0 || input.Replicas > maxProjectReplicas {
		return CreateProjectInput{}, fmt.Errorf("%w: replicas must be between 1 and %d", ErrInvalidInput, maxProjectReplicas)
	}
	if input.Replicas == 0 {
		input.Replicas = 1
	}
	if input.ContainerPort < 0 || input.ContainerPort > 65535 {
		return CreateProjectInput{}, fmt.Errorf("%w: container port must be between 1 and 65535", ErrInvalidInput)
	}
	if input.ContainerPort == 0 {
		input.ContainerPort = 8080
	}

	input.ID = strings.TrimSpace(input.ID)
	if len(input.ID) > maxProjectIDLength || hasControl(input.ID) {
		return CreateProjectInput{}, fmt.Errorf("%w: project id is invalid", ErrInvalidInput)
	}
	input.RepositoryID = strings.TrimSpace(input.RepositoryID)
	if len(input.RepositoryID) > 255 || hasControl(input.RepositoryID) {
		return CreateProjectInput{}, fmt.Errorf("%w: repository id is invalid", ErrInvalidInput)
	}
	input.ImageRepository = strings.TrimSpace(input.ImageRepository)
	if err := validateImageRepository(input.ImageRepository); err != nil {
		return CreateProjectInput{}, err
	}
	input.RegistryConnectionID = strings.TrimSpace(input.RegistryConnectionID)
	if input.RegistryConnectionID != "" && !validRegistryConnectionID(input.RegistryConnectionID) {
		return CreateProjectInput{}, fmt.Errorf("%w: registry connection id is invalid", ErrInvalidInput)
	}
	return input, nil
}

// validateImageRepository accepts the optional project-owned image push
// target. It must be an explicit registry host plus repository path, without
// tag or digest, because the builder stamps its own commit tag and deploys
// the returned digest.
func validateImageRepository(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxProjectImageRepositoryLength || strings.ContainsAny(value, "\x00\r\n@ ") || strings.Contains(value, "://") {
		return fmt.Errorf("%w: image_repository is invalid", ErrInvalidInput)
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		return fmt.Errorf("%w: image_repository must include an explicit registry host", ErrInvalidInput)
	}
	host := strings.ToLower(parts[0])
	if host != parts[0] || (host != "localhost" && !strings.ContainsAny(host, ".:")) {
		return fmt.Errorf("%w: image_repository must include an explicit registry host", ErrInvalidInput)
	}
	if _, err := normalizeRegistryHost(parts[0]); err != nil {
		return fmt.Errorf("%w: image_repository registry host is invalid", ErrInvalidInput)
	}
	for index, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.ContainsAny(part, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || (index > 0 && strings.Contains(part, ":")) {
			return fmt.Errorf("%w: image_repository is invalid", ErrInvalidInput)
		}
	}
	return nil
}

func validateImageRepositoryRegistry(imageRepository string, registry string) error {
	if strings.TrimSpace(registry) == "" {
		return nil
	}
	if strings.TrimSpace(imageRepository) == "" {
		return nil
	}
	host := strings.ToLower(strings.SplitN(strings.TrimSpace(imageRepository), "/", 2)[0])
	if host != strings.ToLower(strings.TrimSpace(registry)) {
		return fmt.Errorf("%w: image repository host must match the selected registry connection", ErrInvalidInput)
	}
	return nil
}

// ValidateCreateProjectInput is used by the HTTP layer before it performs
// provider side effects. The store repeats normalization before persistence.
func ValidateCreateProjectInput(input CreateProjectInput) error {
	_, err := normalizeCreateProjectInput(input)
	return err
}

func validateProjectUpdateInput(input UpdateProjectInput) error {
	if input.RepositoryURL != nil {
		if err := validateRepositoryURL(strings.TrimSpace(*input.RepositoryURL)); err != nil {
			return err
		}
	}
	if input.RepositoryID != nil && (len(strings.TrimSpace(*input.RepositoryID)) > 255 || hasControl(*input.RepositoryID)) {
		return fmt.Errorf("%w: repository id is invalid", ErrInvalidInput)
	}
	if input.Description != nil && !validText(strings.TrimSpace(*input.Description), maxProjectDescriptionLength) {
		return fmt.Errorf("%w: project description is invalid", ErrInvalidInput)
	}
	if input.DefaultBranch != nil && !validGitBranch(strings.TrimSpace(*input.DefaultBranch)) {
		return fmt.Errorf("%w: default branch is invalid", ErrInvalidInput)
	}
	if input.AutoMergeTargetID != nil {
		value := strings.TrimSpace(*input.AutoMergeTargetID)
		if len(value) > 64 || hasControl(value) {
			return fmt.Errorf("%w: auto merge target id is invalid", ErrInvalidInput)
		}
	}
	if input.ClusterID != nil {
		clusterID := strings.TrimSpace(*input.ClusterID)
		if clusterID == "" || len(clusterID) > maxProjectClusterIDLength || hasControl(clusterID) {
			return fmt.Errorf("%w: cluster_id is invalid", ErrInvalidInput)
		}
	}
	if input.Namespace != nil && !validDeploymentNamespace(strings.TrimSpace(*input.Namespace)) {
		return fmt.Errorf("%w: namespace is invalid", ErrInvalidInput)
	}
	if input.Replicas != nil && (*input.Replicas < 1 || *input.Replicas > maxProjectReplicas) {
		return fmt.Errorf("%w: replicas must be between 1 and %d", ErrInvalidInput, maxProjectReplicas)
	}
	if input.ContainerPort != nil && (*input.ContainerPort < 1 || *input.ContainerPort > 65535) {
		return fmt.Errorf("%w: container port must be between 1 and 65535", ErrInvalidInput)
	}
	if input.ImageRepository != nil {
		if err := validateImageRepository(strings.TrimSpace(*input.ImageRepository)); err != nil {
			return err
		}
	}
	if input.RegistryConnectionID != nil {
		value := strings.TrimSpace(*input.RegistryConnectionID)
		if value != "" && !validRegistryConnectionID(value) {
			return fmt.Errorf("%w: registry connection id is invalid", ErrInvalidInput)
		}
	}
	return nil
}

// ValidateUpdateProjectInput lets the HTTP layer reject invalid input before
// registering a replacement repository with the Git provider.
func ValidateUpdateProjectInput(input UpdateProjectInput) error {
	return validateProjectUpdateInput(input)
}

func validateRepositoryURL(value string) error {
	if value == "" {
		return fmt.Errorf("%w: name and repository_url are required", ErrInvalidInput)
	}
	if !validText(value, maxProjectRepositoryLength) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%w: repository_url is invalid", ErrInvalidInput)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("%w: repository_url must be an http or https URL", ErrInvalidInput)
	}
	return nil
}

func validDeployStrategy(value string) bool {
	switch value {
	case "rolling", "canary", "blue_green":
		return true
	default:
		return false
	}
}

func validText(value string, maxRunes int) bool {
	return utf8.ValidString(value) && len([]rune(value)) <= maxRunes && !hasControl(value)
}

// validGitBranch follows the ref-name restrictions that matter at the API
// boundary. Existence is checked by the configured Git provider later.
func validGitBranch(value string) bool {
	if value == "" || len([]rune(value)) > maxProjectBranchLength || hasControl(value) || strings.ContainsAny(value, " ~^:?*[\\") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || value == "@" {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") {
			return false
		}
	}
	return true
}
