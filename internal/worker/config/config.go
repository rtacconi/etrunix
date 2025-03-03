package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// WorkerConfig holds worker configuration
type WorkerConfig struct {
	WorkerID          string
	GRPCPort          int
	AdvertiseAddress  string
	LibvirtURI        string
	VMStoragePath     string
	EtcdEndpoints     []string
	AvailableCPUs     int
	AvailableMemoryMB int64
	AvailableDiskGB   int64
}

// LoadFromEnv loads configuration from environment variables
func LoadFromEnv() (*WorkerConfig, error) {
	cfg := &WorkerConfig{
		WorkerID:          getEnvOrDefault("ETRUNIX_WORKER_ID", ""),
		GRPCPort:          getEnvAsIntOrDefault("ETRUNIX_GRPC_PORT", 50051),
		AdvertiseAddress:  getEnvOrDefault("ETRUNIX_ADVERTISE_ADDRESS", ""),
		LibvirtURI:        getEnvOrDefault("ETRUNIX_LIBVIRT_URI", "qemu:///system"),
		VMStoragePath:     getEnvOrDefault("ETRUNIX_VM_STORAGE_PATH", "/var/lib/etrunix/vms"),
		EtcdEndpoints:     strings.Split(getEnvOrDefault("ETRUNIX_ETCD_ENDPOINTS", "localhost:2379"), ","),
		AvailableCPUs:     getEnvAsIntOrDefault("ETRUNIX_AVAILABLE_CPUS", 4),
		AvailableMemoryMB: getEnvAsInt64OrDefault("ETRUNIX_AVAILABLE_MEMORY_MB", 8192),
		AvailableDiskGB:   getEnvAsInt64OrDefault("ETRUNIX_AVAILABLE_DISK_GB", 100),
	}

	// Generate worker ID if not provided
	if cfg.WorkerID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("failed to get hostname: %v", err)
		}
		cfg.WorkerID = hostname
	}

	// Get advertise address if not provided
	if cfg.AdvertiseAddress == "" {
		// In a real implementation, we would determine the best IP to use
		cfg.AdvertiseAddress = "localhost"
	}

	return cfg, nil
}

// getEnvOrDefault gets an environment variable or returns a default value
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvAsIntOrDefault gets an environment variable as int or returns a default value
func getEnvAsIntOrDefault(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsInt64OrDefault gets an environment variable as int64 or returns a default value
func getEnvAsInt64OrDefault(key string, defaultValue int64) int64 {
	if value, exists := os.LookupEnv(key); exists {
		if int64Value, err := strconv.ParseInt(value, 10, 64); err == nil {
			return int64Value
		}
	}
	return defaultValue
}
