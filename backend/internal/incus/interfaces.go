package incus

import (
	"io"

	"github.com/gorilla/websocket"
	incusapi "github.com/lxc/incus/v6/shared/api"
)

// IncusClient abstracts Incus server operations for testing.
type IncusClient interface {
	// Instances
	CreateInstance(name, image string, profiles []string, config map[string]string, devices map[string]map[string]string) error
	StartInstance(name string) error
	StopInstance(name string) error
	DeleteInstance(name string) error
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

	// Server info
	GetServerResources() (*incusapi.Resources, error)
}
