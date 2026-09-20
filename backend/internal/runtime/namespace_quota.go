package runtime

import (
	"fmt"
	"strings"

	"github.com/yuebuy/cicd-platform/backend/internal/domain"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	namespaceResourceQuotaName = "ttp-resource-quota"
	namespaceLimitRangeName    = "ttp-limit-range"
)

type namespaceQuotaQuantities struct {
	cpuRequest                     resource.Quantity
	cpuLimit                       resource.Quantity
	memoryRequest                  resource.Quantity
	memoryLimit                    resource.Quantity
	ephemeralStorageRequest        resource.Quantity
	ephemeralStorageLimit          resource.Quantity
	storage                        resource.Quantity
	defaultCPURequest              resource.Quantity
	defaultCPULimit                resource.Quantity
	defaultMemoryRequest           resource.Quantity
	defaultMemoryLimit             resource.Quantity
	defaultEphemeralStorageRequest resource.Quantity
	defaultEphemeralStorageLimit   resource.Quantity
}

func parseNamespaceQuota(quota domain.NamespaceQuota) (namespaceQuotaQuantities, error) {
	values := map[string]string{
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
	parsed := make(map[string]resource.Quantity, len(values))
	for name, value := range values {
		quantity, err := resource.ParseQuantity(strings.TrimSpace(value))
		if err != nil || quantity.Sign() <= 0 {
			if err == nil {
				err = fmt.Errorf("must be greater than zero")
			}
			return namespaceQuotaQuantities{}, fmt.Errorf("%w: namespace quota %s %q: %v", ErrInvalidKubernetesInput, name, value, err)
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
		{"default_cpu_limit", "cpu_limit"},
		{"default_memory_limit", "memory_limit"},
		{"default_ephemeral_storage_limit", "ephemeral_storage_limit"},
		{"default_cpu_request", "cpu_request"},
		{"default_memory_request", "memory_request"},
		{"default_ephemeral_storage_request", "ephemeral_storage_request"},
	} {
		left, right := parsed[pair[0]], parsed[pair[1]]
		if (&left).Cmp(right) > 0 {
			return namespaceQuotaQuantities{}, fmt.Errorf("%w: namespace quota %s cannot exceed %s", ErrInvalidKubernetesInput, pair[0], pair[1])
		}
	}
	if quota.Pods < 1 || quota.Pods > 10000 {
		return namespaceQuotaQuantities{}, fmt.Errorf("%w: namespace quota pods must be between 1 and 10000", ErrInvalidKubernetesInput)
	}
	if quota.PersistentVolumeClaims < 1 || quota.PersistentVolumeClaims > 1000 {
		return namespaceQuotaQuantities{}, fmt.Errorf("%w: namespace quota persistent_volume_claims must be between 1 and 1000", ErrInvalidKubernetesInput)
	}
	return namespaceQuotaQuantities{
		cpuRequest:                     parsed["cpu_request"],
		cpuLimit:                       parsed["cpu_limit"],
		memoryRequest:                  parsed["memory_request"],
		memoryLimit:                    parsed["memory_limit"],
		ephemeralStorageRequest:        parsed["ephemeral_storage_request"],
		ephemeralStorageLimit:          parsed["ephemeral_storage_limit"],
		storage:                        parsed["storage"],
		defaultCPURequest:              parsed["default_cpu_request"],
		defaultCPULimit:                parsed["default_cpu_limit"],
		defaultMemoryRequest:           parsed["default_memory_request"],
		defaultMemoryLimit:             parsed["default_memory_limit"],
		defaultEphemeralStorageRequest: parsed["default_ephemeral_storage_request"],
		defaultEphemeralStorageLimit:   parsed["default_ephemeral_storage_limit"],
	}, nil
}
