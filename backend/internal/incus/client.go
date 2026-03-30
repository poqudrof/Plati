package incus

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gorilla/websocket"
	incusclient "github.com/lxc/incus/v6/client"
	incusapi "github.com/lxc/incus/v6/shared/api"
)

type Client struct {
	server incusclient.InstanceServer
	name   string
}

func NewClient(name, endpoint, certPath, keyPath string) (*Client, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read client cert %s: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read client key %s: %w", keyPath, err)
	}

	args := &incusclient.ConnectionArgs{
		TLSClientCert:      string(certPEM),
		TLSClientKey:       string(keyPEM),
		InsecureSkipVerify: true,
		SkipGetServer:      true,
	}

	server, err := incusclient.ConnectIncus(endpoint, args)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", name, err)
	}

	return &Client{server: server, name: name}, nil
}

func (c *Client) CreateInstance(name, image string, profiles []string, config map[string]string, devices map[string]map[string]string) error {
	req := incusapi.InstancesPost{
		Name: name,
		Source: incusapi.InstanceSource{
			Type:  "image",
			Alias: image,
		},
		InstancePut: incusapi.InstancePut{
			Profiles: profiles,
			Config:   config,
			Devices:  devices,
		},
	}

	// Handle remote images (e.g. "images:ubuntu/24.04")
	if parts := strings.SplitN(image, ":", 2); len(parts) == 2 {
		remoteName := parts[0]
		alias := parts[1]
		req.Source.Alias = alias

		remoteURL, ok := remoteServers[remoteName]
		if !ok {
			return fmt.Errorf("unknown image remote %q", remoteName)
		}

		imgServer, err := incusclient.ConnectSimpleStreams(remoteURL, nil)
		if err != nil {
			return fmt.Errorf("connect to image server %s: %w", remoteName, err)
		}

		imgAlias, _, err := imgServer.GetImageAlias(alias)
		if err != nil {
			return fmt.Errorf("find image alias %s: %w", alias, err)
		}

		img, _, err := imgServer.GetImage(imgAlias.Target)
		if err != nil {
			return fmt.Errorf("get image %s: %w", alias, err)
		}

		op, err := c.server.CreateInstanceFromImage(imgServer, *img, req)
		if err != nil {
			return fmt.Errorf("create instance %s: %w", name, err)
		}
		return op.Wait()
	}

	op, err := c.server.CreateInstance(req)
	if err != nil {
		return fmt.Errorf("create instance %s: %w", name, err)
	}
	return op.Wait()
}

var remoteServers = map[string]string{
	"images": "https://images.linuxcontainers.org",
}

func (c *Client) StartInstance(name string) error {
	reqState := incusapi.InstanceStatePut{
		Action:  "start",
		Timeout: -1,
	}
	op, err := c.server.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return fmt.Errorf("start instance %s: %w", name, err)
	}
	return op.Wait()
}

func (c *Client) StopInstance(name string) error {
	reqState := incusapi.InstanceStatePut{
		Action:  "stop",
		Timeout: 30,
		Force:   true,
	}
	op, err := c.server.UpdateInstanceState(name, reqState, "")
	if err != nil {
		return fmt.Errorf("stop instance %s: %w", name, err)
	}
	return op.Wait()
}

func (c *Client) DeleteInstance(name string) error {
	op, err := c.server.DeleteInstance(name)
	if err != nil {
		return fmt.Errorf("delete instance %s: %w", name, err)
	}
	return op.Wait()
}

func (c *Client) GetInstanceState(name string) (*incusapi.InstanceState, error) {
	state, _, err := c.server.GetInstanceState(name)
	if err != nil {
		return nil, fmt.Errorf("get instance state %s: %w", name, err)
	}
	return state, nil
}

func (c *Client) GetInstance(name string) (*incusapi.Instance, error) {
	inst, _, err := c.server.GetInstance(name)
	if err != nil {
		return nil, fmt.Errorf("get instance %s: %w", name, err)
	}
	return inst, nil
}

func (c *Client) CreateVolume(pool, name string, sizeGB int) error {
	vol := incusapi.StorageVolumesPost{
		Name: name,
		Type: "custom",
		StorageVolumePut: incusapi.StorageVolumePut{
			Config: map[string]string{
				"size": fmt.Sprintf("%dGiB", sizeGB),
			},
		},
	}
	return c.server.CreateStoragePoolVolume(pool, vol)
}

func (c *Client) DeleteVolume(pool, name string) error {
	return c.server.DeleteStoragePoolVolume(pool, "custom", name)
}

func (c *Client) AttachVolume(pool, volumeName, instanceName, deviceName, path string) error {
	inst, etag, err := c.server.GetInstance(instanceName)
	if err != nil {
		return fmt.Errorf("get instance for volume attach: %w", err)
	}

	if inst.Devices == nil {
		inst.Devices = map[string]map[string]string{}
	}
	inst.Devices[deviceName] = map[string]string{
		"type":   "disk",
		"pool":   pool,
		"source": volumeName,
		"path":   path,
	}

	op, err := c.server.UpdateInstance(instanceName, inst.Writable(), etag)
	if err != nil {
		return fmt.Errorf("attach volume: %w", err)
	}
	return op.Wait()
}

func (c *Client) DetachVolume(instanceName, deviceName string) error {
	inst, etag, err := c.server.GetInstance(instanceName)
	if err != nil {
		return fmt.Errorf("get instance for volume detach: %w", err)
	}

	delete(inst.Devices, deviceName)

	op, err := c.server.UpdateInstance(instanceName, inst.Writable(), etag)
	if err != nil {
		return fmt.Errorf("detach volume: %w", err)
	}
	return op.Wait()
}

func (c *Client) ListImages() ([]incusapi.Image, error) {
	images, err := c.server.GetImages()
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}
	return images, nil
}

func (c *Client) ExecInstance(name string, command []string, env map[string]string, stdin io.ReadCloser, stdout io.WriteCloser, control func(conn *websocket.Conn)) error {
	req := incusapi.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: true,
		Environment: env,
		Width:       80,
		Height:      24,
	}

	args := &incusclient.InstanceExecArgs{
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stdout,
		Control:  control,
		DataDone: make(chan bool),
	}

	op, err := c.server.ExecInstance(name, req, args)
	if err != nil {
		return fmt.Errorf("exec in %s: %w", name, err)
	}

	if err := op.Wait(); err != nil {
		return fmt.Errorf("exec wait %s: %w", name, err)
	}

	<-args.DataDone
	return nil
}

func (c *Client) GetServerResources() (*incusapi.Resources, error) {
	resources, err := c.server.GetServerResources()
	if err != nil {
		return nil, fmt.Errorf("get server resources: %w", err)
	}
	return resources, nil
}
