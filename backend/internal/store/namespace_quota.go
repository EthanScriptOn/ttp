package store

import (
	"fmt"
	"strings"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"k8s.io/apimachinery/pkg/api/resource"
)

// The default profile is deliberately bounded. It is large enough for a
// small development environment, but a newly-created TTP namespace never
// starts with unlimited CPU, memory, disk or PVC capacity.
var defaultNamespaceQuota = domain.NamespaceQuota{
	CPURequest:                     "2",
	CPULimit:                       "4",
	MemoryRequest:                  "2Gi",
	MemoryLimit:                    "4Gi",
	EphemeralStorageRequest:        "10Gi",
	EphemeralStorageLimit:          "20Gi",
	Storage:                        "50Gi",
	Pods:                           20,
	PersistentVolumeClaims:         10,
	DefaultCPURequest:              "100m",
	DefaultCPULimit:                "500m",
	DefaultMemoryRequest:           "128Mi",
	DefaultMemoryLimit:             "512Mi",
	DefaultEphemeralStorageRequest: "256Mi",
	DefaultEphemeralStorageLimit:   "1Gi",
}

// DefaultNamespaceQuota returns a copy of the bounded namespace profile used
// when an environment is created without an explicit quota.
func DefaultNamespaceQuota() domain.NamespaceQuota {
	return defaultNamespaceQuota
}

// NormalizeNamespaceQuota fills omitted values from the bounded default
// profile and validates Kubernetes quantity syntax and ordering constraints.
func NormalizeNamespaceQuota(input *domain.NamespaceQuota) (domain.NamespaceQuota, error) {
	quota := defaultNamespaceQuota
	if input != nil {
		quota = *input
	}

	quota.CPURequest = quantityOrDefault(quota.CPURequest, defaultNamespaceQuota.CPURequest)
	quota.CPULimit = quantityOrDefault(quota.CPULimit, defaultNamespaceQuota.CPULimit)
	quota.MemoryRequest = quantityOrDefault(quota.MemoryRequest, defaultNamespaceQuota.MemoryRequest)
	quota.MemoryLimit = quantityOrDefault(quota.MemoryLimit, defaultNamespaceQuota.MemoryLimit)
	quota.EphemeralStorageRequest = quantityOrDefault(quota.EphemeralStorageRequest, defaultNamespaceQuota.EphemeralStorageRequest)
	quota.EphemeralStorageLimit = quantityOrDefault(quota.EphemeralStorageLimit, defaultNamespaceQuota.EphemeralStorageLimit)
	quota.Storage = quantityOrDefault(quota.Storage, defaultNamespaceQuota.Storage)
	quota.DefaultCPURequest = quantityOrDefault(quota.DefaultCPURequest, defaultNamespaceQuota.DefaultCPURequest)
	quota.DefaultCPULimit = quantityOrDefault(quota.DefaultCPULimit, defaultNamespaceQuota.DefaultCPULimit)
	quota.DefaultMemoryRequest = quantityOrDefault(quota.DefaultMemoryRequest, defaultNamespaceQuota.DefaultMemoryRequest)
	quota.DefaultMemoryLimit = quantityOrDefault(quota.DefaultMemoryLimit, defaultNamespaceQuota.DefaultMemoryLimit)
	quota.DefaultEphemeralStorageRequest = quantityOrDefault(quota.DefaultEphemeralStorageRequest, defaultNamespaceQuota.DefaultEphemeralStorageRequest)
	quota.DefaultEphemeralStorageLimit = quantityOrDefault(quota.DefaultEphemeralStorageLimit, defaultNamespaceQuota.DefaultEphemeralStorageLimit)
	if quota.Pods == 0 {
		quota.Pods = defaultNamespaceQuota.Pods
	}
	if quota.PersistentVolumeClaims == 0 {
		quota.PersistentVolumeClaims = defaultNamespaceQuota.PersistentVolumeClaims
	}

	if err := validateNamespaceQuota(quota); err != nil {
		return domain.NamespaceQuota{}, err
	}
	return quota, nil
}

func NamespaceQuotasEqual(left, right domain.NamespaceQuota) bool {
	return left == right
}

func namespaceQuotaKey(spaceID, clusterID, environment string) string {
	return strings.Join([]string{strings.TrimSpace(spaceID), strings.TrimSpace(clusterID), strings.ToLower(strings.TrimSpace(environment))}, "\x00")
}

func quantityOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func validateNamespaceQuota(quota domain.NamespaceQuota) error {
	quantities := map[string]string{
		"cpu_request":                       quota.CPURequest,
		"cpu_limit":                         quota.CPULimit,
		"memory_request":                    quota.MemoryRequest,
		"memory_limit":                      quota.MemoryLimit,
		"ephemeral_storage_request":         quota.EphemeralStorageRequest,
		"ephemeral_storage_limit":           quota.EphemeralStorageLimit,
		"storage":                           quota.Storage,
		"default_cpu_request":               quota.DefaultCPURequest,
		"default_cpu_limit":                 quota.DefaultCPULimit,
		"default_memory_request":            quota.DefaultMemoryRequest,
		"default_memory_limit":              quota.DefaultMemoryLimit,
		"default_ephemeral_storage_request": quota.DefaultEphemeralStorageRequest,
		"default_ephemeral_storage_limit":   quota.DefaultEphemeralStorageLimit,
	}
	parsed := make(map[string]resource.Quantity, len(quantities))
	for name, value := range quantities {
		quantity, err := resource.ParseQuantity(strings.TrimSpace(value))
		if err != nil || quantity.Sign() <= 0 {
			if err == nil {
				err = fmt.Errorf("must be greater than zero")
			}
			return fmt.Errorf("%w: %s %q: %v", ErrInvalidInput, name, value, err)
		}
		parsed[name] = quantity
	}
	for _, pair := range [][2]string{
		{"cpu_request", "cpu_limit"},
		{"memory_request", "memory_limit"},
		{"ephemeral_storage_request", "ephemeral_storage_limit"},
		{"default_cpu_request", "default_cpu_limit"},
		{"default_memory_request", "default_memory_limit"},
		{"default_ephemeral_storage_request", "default_ephemeral_storage_limit"},
	} {
		left, right := parsed[pair[0]], parsed[pair[1]]
		if (&left).Cmp(right) > 0 {
			return fmt.Errorf("%w: %s cannot exceed %s", ErrInvalidInput, pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{"default_cpu_limit", "cpu_limit"},
		{"default_memory_limit", "memory_limit"},
		{"default_ephemeral_storage_limit", "ephemeral_storage_limit"},
		{"default_cpu_request", "cpu_request"},
		{"default_memory_request", "memory_request"},
		{"default_ephemeral_storage_request", "ephemeral_storage_request"},
	} {
		left, right := parsed[pair[0]], parsed[pair[1]]
		if (&left).Cmp(right) > 0 {
			return fmt.Errorf("%w: %s cannot exceed namespace %s", ErrInvalidInput, pair[0], pair[1])
		}
	}
	if quota.Pods < 1 || quota.Pods > 10000 {
		return fmt.Errorf("%w: pods must be between 1 and 10000", ErrInvalidInput)
	}
	if quota.PersistentVolumeClaims < 1 || quota.PersistentVolumeClaims > 1000 {
		return fmt.Errorf("%w: persistent_volume_claims must be between 1 and 1000", ErrInvalidInput)
	}
	return nil
}
