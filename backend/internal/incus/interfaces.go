package incus

import (
	"io"
	"time"

	"github.com/gorilla/websocket"
	incusapi "github.com/lxc/incus/v6/shared/api"
)

// FileEntry represents a file or directory inside an instance (SVAR-compatible shape).
type FileEntry struct {
	ID   string `json:"id"`   // full path
	Name string `json:"name"` // basename
	Type string `json:"type"` // "file" or "folder"
	Size int64  `json:"size"`
	Date int64  `json:"date"` // unix timestamp
}

// FileInfo holds metadata about a single file retrieved from an instance.
type FileInfo struct {
	UID  int64
	GID  int64
	Mode int
	Type string // "file" or "directory"
}

// VolumeSnapshotInfo represents a storage volume snapshot.
type VolumeSnapshotInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// IncusClient abstracts Incus server operations for testing.
type IncusClient interface {
	// Instances
	CreateInstance(name, image string, profiles []string, config map[string]string, devices map[string]map[string]string) error
	StartInstance(name string) error
	StopInstance(name string) error
	DeleteInstance(name string) error
	// RenameInstance changes an instance's Incus name. Incus refuses this while
	// the instance is running, so the caller must stop it first.
	RenameInstance(name, newName string) error
	GetInstanceState(name string) (*incusapi.InstanceState, error)
	GetInstance(name string) (*incusapi.Instance, error)

	// Volumes
	CreateVolume(pool, name string, sizeGB int) error
	DeleteVolume(pool, name string) error
	AttachVolume(pool, volumeName, instanceName, deviceName, path string) error
	DetachVolume(instanceName, deviceName string) error

	// Images
	ListImages() ([]incusapi.Image, error)

	// Exec
	ExecInstance(name string, command []string, env map[string]string, stdin io.ReadCloser, stdout io.WriteCloser, control func(conn *websocket.Conn)) error
	RunCommand(name string, command []string) (string, error)
	// StreamCommand runs a command and returns its stdout as a streaming reader (for piping).
	StreamCommand(name string, command []string) (io.ReadCloser, error)

	// Files
	PushFile(instanceName, remotePath string, content []byte, uid, gid int64, mode int) error

	// Config
	UpdateInstanceConfig(name string, config map[string]string) error

	// Devices
	AttachHostPath(instanceName, deviceName, hostPath, instancePath string) error

	// Server info
	GetServerResources() (*incusapi.Resources, error)
	GetProfileNames() ([]string, error)

	// Storage file operations
	ListDirectory(instanceName, path string) ([]FileEntry, error)
	GetFile(instanceName, path string) (io.ReadCloser, *FileInfo, error)
	StreamDirectory(instanceName, path string) (io.ReadCloser, error)

	// Volume snapshots
	ListVolumeSnapshots(pool, volumeName string) ([]VolumeSnapshotInfo, error)
	CreateVolumeSnapshot(pool, volumeName, snapshotName string) error
	DeleteVolumeSnapshot(pool, volumeName, snapshotName string) error
	RestoreVolumeSnapshot(pool, volumeName, snapshotName string) error
}
