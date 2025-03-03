package vm

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/libvirt/libvirt-go"
)

// Status represents VM status
type Status int

const (
	StatusUnknown Status = iota
	StatusRunning
	StatusStopped
	StatusPaused
)

// CreateVMParams contains parameters for VM creation
type CreateVMParams struct {
	Name       string
	CPUCores   int
	MemoryMB   int
	DiskSizeGB int
	Image      string
}

// Manager handles VM operations
type Manager struct {
	conn         *libvirt.Connect
	storagePath  string
}

// NewManager creates a new VM manager
func NewManager(conn *libvirt.Connect, storagePath string) *Manager {
	// Ensure storage path exists
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		log.Printf("Failed to create storage path: %v", err)
	}
	
	return &Manager{
		conn:        conn,
		storagePath: storagePath,
	}
}

// CreateVM creates a new VM
func (m *Manager) CreateVM(ctx context.Context, params *CreateVMParams) (string, error) {
	// Generate a unique ID for the VM
	vmID := uuid.New().String()
	
	// Create VM disk
	diskPath, err := m.createDisk(vmID, params.DiskSizeGB, params.Image)
	if err != nil {
		return "", fmt.Errorf("failed to create disk: %v", err)
	}
	
	// Create VM XML definition
	xmlConfig := m.generateVMXML(vmID, params.Name, params.CPUCores, params.MemoryMB, diskPath)
	
	// Define the domain
	domain, err := m.conn.DomainDefineXML(xmlConfig)
	if err != nil {
		return "", fmt.Errorf("failed to define domain: %v", err)
	}
	defer domain.Free()
	
	// Start the domain
	if err := domain.Create(); err != nil {
		return "", fmt.Errorf("failed to start domain: %v", err)
	}
	
	log.Printf("Created and started VM: %s (ID: %s)", params.Name, vmID)
	return vmID, nil
}

// StopVM stops a VM
func (m *Manager) StopVM(vmID string) error {
	domain, err := m.conn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return fmt.Errorf("failed to find domain: %v", err)
	}
	defer domain.Free()
	
	if err := domain.Shutdown(); err != nil {
		// Try to force off if graceful shutdown fails
		if forceErr := domain.Destroy(); forceErr != nil {
			return fmt.Errorf("failed to force stop domain: %v", forceErr)
		}
	}
	
	return nil
}

// GetVMStatus returns the status of a VM
func (m *Manager) GetVMStatus(vmID string) (Status, error) {
	domain, err := m.conn.LookupDomainByUUIDString(vmID)
	if err != nil {
		return StatusUnknown, fmt.Errorf("failed to find domain: %v", err)
	}
	defer domain.Free()
	
	state, _, err := domain.GetState()
	if err != nil {
		return StatusUnknown, fmt.Errorf("failed to get domain state: %v", err)
	}
	
	switch state {
	case libvirt.DOMAIN_RUNNING:
		return StatusRunning, nil
	case libvirt.DOMAIN_PAUSED:
		return StatusPaused, nil
	case libvirt.DOMAIN_SHUTDOWN, libvirt.DOMAIN_SHUTOFF:
		return StatusStopped, nil
	default:
		return StatusUnknown, nil
	}
}

// createDisk creates a disk for the VM from the template image
func (m *Manager) createDisk(vmID string, sizeGB int, imageURL string) (string, error) {
	diskPath := filepath.Join(m.storagePath, fmt.Sprintf("%s.qcow2", vmID))
	
	// Download image if it's a URL
	var sourcePath string
	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		// Download to temp file
		tempPath := filepath.Join(m.storagePath, "temp_" + uuid.New().String())
		cmd := exec.Command("curl", "-L", "-o", tempPath, imageURL)
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("failed to download image: %v", err)
		}
		sourcePath = tempPath
		defer os.Remove(tempPath)
	} else {
		// Use local path
		sourcePath = imageURL
	}
	
	// Create disk from image
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", "-o", fmt.Sprintf("backing_file=%s", sourcePath), diskPath, fmt.Sprintf("%dG", sizeGB))
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to create disk: %v, output: %s", err, string(out))
	}
	
	return diskPath, nil
}

// generateVMXML creates XML configuration for the VM
func (m *Manager) generateVMXML(vmID, name string, cpuCores, memoryMB int, diskPath string) string {
	return fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <uuid>%s</uuid>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64' machine='pc-q35-6.2'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
  </features>
  <cpu mode='host-model'/>
  <clock offset='utc'/>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='bridge'>
      <source bridge='virbr0'/>
      <model type='virtio'/>
    </interface>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <graphics type='vnc' port='-1' autoport='yes' listen='0.0.0.0'>
      <listen type='address' address='0.0.0.0'/>
    </graphics>
    <video>
      <model type='qxl' ram='65536' vram='65536' vgamem='16384' heads='1'/>
    </video>
  </devices>
</domain>`, name, vmID, memoryMB, cpuCores, diskPath)
}
