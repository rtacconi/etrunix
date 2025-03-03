package worker

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/libvirt/libvirt-go"
	"google.golang.org/grpc"

	"etrunix/internal/worker/agent"
	"etrunix/internal/worker/config"
	"etrunix/internal/worker/vm"
	pb "etrunix/pkg/proto"
)

// Worker represents a node that can run VMs
type Worker struct {
	config     *config.WorkerConfig
	libvirtConn *libvirt.Connect
	grpcServer *grpc.Server
	agent      *agent.Agent
	vmManager  *vm.Manager
}

// NewWorker creates a new worker instance
func NewWorker(cfg *config.WorkerConfig) (*Worker, error) {
	// Connect to libvirt
	conn, err := libvirt.NewConnect(cfg.LibvirtURI)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %v", err)
	}

	// Create VM manager
	vmManager := vm.NewManager(conn, cfg.VMStoragePath)
	
	// Create the agent
	agent, err := agent.NewAgent(cfg.EtcdEndpoints, cfg.WorkerID)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to create agent: %v", err)
	}

	w := &Worker{
		config:     cfg,
		libvirtConn: conn,
		vmManager:  vmManager,
		agent:      agent,
	}

	return w, nil
}

// Start runs the worker
func (w *Worker) Start(ctx context.Context) error {
	// Start reporting worker status to etcd
	go w.agent.Start(ctx)

	// Start gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", w.config.GRPCPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	w.grpcServer = grpc.NewServer()
	pb.RegisterWorkerServiceServer(w.grpcServer, w)

	log.Printf("Worker %s started, listening on %d", w.config.WorkerID, w.config.GRPCPort)
	
	// Update initial status
	hostname, _ := os.Hostname()
	w.agent.UpdateStatus(&pb.WorkerStatus{
		Id:          w.config.WorkerID,
		Hostname:    hostname,
		IpAddress:   w.config.AdvertiseAddress,
		CpuCores:    int32(w.config.AvailableCPUs),
		MemoryTotal: w.config.AvailableMemoryMB,
		DiskTotal:   w.config.AvailableDiskGB,
		IsHealthy:   true,
		LastUpdated: time.Now().Unix(),
	})

	return w.grpcServer.Serve(lis)
}

// Stop gracefully shuts down the worker
func (w *Worker) Stop() {
	if w.grpcServer != nil {
		w.grpcServer.GracefulStop()
	}
	
	if w.agent != nil {
		w.agent.Stop()
	}
	
	if w.libvirtConn != nil {
		w.libvirtConn.Close()
	}
}

// CreateVM implements the Worker service CreateVM RPC
func (w *Worker) CreateVM(ctx context.Context, req *pb.CreateVMRequest) (*pb.CreateVMResponse, error) {
	log.Printf("Received request to create VM: %s", req.Name)
	
	// Validate request
	if req.Name == "" || req.CpuCores == 0 || req.MemoryMb == 0 || req.DiskSizeGb == 0 || req.Image == "" {
		return nil, fmt.Errorf("invalid VM request parameters")
	}
	
	// Check if we have enough resources
	status := w.agent.GetCurrentStatus()
	if status.CpuCores < req.CpuCores || 
	   status.MemoryTotal < req.MemoryMb || 
	   status.DiskTotal < req.DiskSizeGb {
		return nil, fmt.Errorf("insufficient resources to create VM")
	}
	
	// Create the VM using the VM Manager
	vmID, err := w.vmManager.CreateVM(ctx, &vm.CreateVMParams{
		Name:      req.Name,
		CPUCores:  int(req.CpuCores),
		MemoryMB:  int(req.MemoryMb),
		DiskSizeGB: int(req.DiskSizeGb),
		Image:     req.Image,
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %v", err)
	}
	
	// Update worker status
	w.agent.UpdateResourceUsage(int(req.CpuCores), int(req.MemoryMb), int(req.DiskSizeGb))
	
	return &pb.CreateVMResponse{
		VmId: vmID,
		WorkerId: w.config.WorkerID,
		Status: pb.VMStatus_RUNNING,
	}, nil
}

// StopVM implements the Worker service StopVM RPC
func (w *Worker) StopVM(ctx context.Context, req *pb.StopVMRequest) (*pb.StopVMResponse, error) {
	log.Printf("Received request to stop VM: %s", req.VmId)
	
	err := w.vmManager.StopVM(req.VmId)
	if err != nil {
		return nil, fmt.Errorf("failed to stop VM: %v", err)
	}
	
	return &pb.StopVMResponse{
		VmId: req.VmId,
		Success: true,
	}, nil
}

// GetVMStatus implements the Worker service GetVMStatus RPC
func (w *Worker) GetVMStatus(ctx context.Context, req *pb.GetVMStatusRequest) (*pb.GetVMStatusResponse, error) {
	status, err := w.vmManager.GetVMStatus(req.VmId)
	if err != nil {
		return nil, fmt.Errorf("failed to get VM status: %v", err)
	}
	
	var pbStatus pb.VMStatus
	switch status {
	case vm.StatusRunning:
		pbStatus = pb.VMStatus_RUNNING
	case vm.StatusStopped:
		pbStatus = pb.VMStatus_STOPPED
	case vm.StatusPaused:
		pbStatus = pb.VMStatus_PAUSED
	default:
		pbStatus = pb.VMStatus_UNKNOWN
	}
	
	return &pb.GetVMStatusResponse{
		VmId: req.VmId,
		Status: pbStatus,
	}, nil
}
