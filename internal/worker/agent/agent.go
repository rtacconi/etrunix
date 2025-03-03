package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"go.etcd.io/etcd/client/v3"
	
	pb "etrunix/pkg/proto"
)

const (
	updateInterval = 10 * time.Second
	workerTTL      = 30 // seconds
)

// Agent represents a worker agent that reports to etcd
type Agent struct {
	etcdClient    *clientv3.Client
	workerID      string
	keyPrefix     string
	currentStatus *pb.WorkerStatus
	stopCh        chan struct{}
	leaseID       clientv3.LeaseID
}

// NewAgent creates a new agent instance
func NewAgent(endpoints []string, workerID string) (*Agent, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %v", err)
	}

	return &Agent{
		etcdClient:    cli,
		workerID:      workerID,
		keyPrefix:     fmt.Sprintf("/etrunix/workers/%s", workerID),
		currentStatus: &pb.WorkerStatus{Id: workerID},
		stopCh:        make(chan struct{}),
	}, nil
}

// Start begins the agent reporting cycle
func (a *Agent) Start(ctx context.Context) {
	go a.reportingLoop(ctx)
}

// Stop halts the agent
func (a *Agent) Stop() {
	close(a.stopCh)
	
	if a.leaseID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.etcdClient.Revoke(ctx, a.leaseID)
	}
	
	a.etcdClient.Close()
}

// UpdateStatus updates the current worker status
func (a *Agent) UpdateStatus(status *pb.WorkerStatus) {
	a.currentStatus = status
}

// GetCurrentStatus returns the current worker status
func (a *Agent) GetCurrentStatus() *pb.WorkerStatus {
	return a.currentStatus
}

// UpdateResourceUsage updates the resource usage in the status
func (a *Agent) UpdateResourceUsage(cpuUsed, memoryUsed, diskUsed int) {
	a.currentStatus.CpuUsed += int32(cpuUsed)
	a.currentStatus.MemoryUsed += int64(memoryUsed)
	a.currentStatus.DiskUsed += int64(diskUsed)
}

// reportingLoop continuously reports worker status to etcd
func (a *Agent) reportingLoop(ctx context.Context) {
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.reportStatus(ctx); err != nil {
				log.Printf("Failed to report status: %v", err)
			}
		}
	}
}

// reportStatus updates the worker status in etcd
func (a *Agent) reportStatus(ctx context.Context) error {
	// Update timestamp
	a.currentStatus.LastUpdated = time.Now().Unix()
	
	// Create a lease
	lease, err := a.etcdClient.Grant(ctx, workerTTL)
	if err != nil {
		return fmt.Errorf("failed to create lease: %v", err)
	}
	
	a.leaseID = lease.ID
	
	// Marshal the status
	statusJSON, err := json.Marshal(a.currentStatus)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %v", err)
	}
	
	// Put the status with lease
	_, err = a.etcdClient.Put(ctx, a.keyPrefix, string(statusJSON), clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("failed to put status: %v", err)
	}
	
	// Keep the lease alive
	keepAliveCh, err := a.etcdClient.KeepAlive(ctx, lease.ID)
	if err != nil {
		return fmt.Errorf("failed to keep lease alive: %v", err)
	}
	
	// Consume keep alive responses to prevent channel from blocking
	go func() {
		for {
			select {
			case <-a.stopCh:
				return
			case <-ctx.Done():
				return
			case <-keepAliveCh:
				// Just consume the response
			}
		}
	}()
	
	return nil
}
