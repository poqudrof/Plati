package incus

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	args := &incusclient.ConnectionArgs{
		InsecureSkipVerify: true,
		SkipGetServer:      true,
	}

	if certPath != "" && keyPath != "" {
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("read client cert %s: %w", certPath, err)
		}
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read client key %s: %w", keyPath, err)
		}
		args.TLSClientCert = string(certPEM)
		args.TLSClientKey = string(keyPEM)
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

// AttachHostPath bind-mounts a host filesystem path into the instance (read-only).
func (c *Client) AttachHostPath(instanceName, deviceName, hostPath, instancePath string) error {
	inst, etag, err := c.server.GetInstance(instanceName)
	if err != nil {
		return fmt.Errorf("get instance for host path attach: %w", err)
	}

	if inst.Devices == nil {
		inst.Devices = map[string]map[string]string{}
	}
	inst.Devices[deviceName] = map[string]string{
		"type":     "disk",
		"source":   hostPath,
		"path":     instancePath,
		"readonly": "true",
	}

	op, err := c.server.UpdateInstance(instanceName, inst.Writable(), etag)
	if err != nil {
		return fmt.Errorf("attach host path: %w", err)
	}
	return op.Wait()
}

// RenameInstance renames the Incus instance. Incus only allows this while the
// instance is stopped; a running one comes back as an error from the operation.
func (c *Client) RenameInstance(name, newName string) error {
	op, err := c.server.RenameInstance(name, incusapi.InstancePost{Name: newName})
	if err != nil {
		return fmt.Errorf("rename instance %s: %w", name, err)
	}
	if err := op.Wait(); err != nil {
		return fmt.Errorf("rename instance %s: %w", name, err)
	}
	return nil
}

func (c *Client) UpdateInstanceConfig(name string, config map[string]string) error {
	inst, etag, err := c.server.GetInstance(name)
	if err != nil {
		return fmt.Errorf("get instance for config update: %w", err)
	}

	if inst.Config == nil {
		inst.Config = map[string]string{}
	}
	for k, v := range config {
		if v == "" {
			delete(inst.Config, k)
		} else {
			inst.Config[k] = v
		}
	}

	op, err := c.server.UpdateInstance(name, inst.Writable(), etag)
	if err != nil {
		return fmt.Errorf("update instance config: %w", err)
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

// RunCommand executes a command in a non-interactive session and returns combined stdout+stderr.
func (c *Client) RunCommand(name string, command []string) (string, error) {
	pr, pw := io.Pipe()

	req := incusapi.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: false,
		Environment: map[string]string{},
	}

	dataDone := make(chan bool)
	args := &incusclient.InstanceExecArgs{
		Stdin:    io.NopCloser(bytes.NewReader(nil)),
		Stdout:   pw,
		Stderr:   pw,
		DataDone: dataDone,
	}

	op, err := c.server.ExecInstance(name, req, args)
	if err != nil {
		pr.Close()
		pw.Close()
		return "", fmt.Errorf("exec %s: %w", name, err)
	}

	outCh := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(pr)
		outCh <- data
	}()

	opErr := op.Wait()
	<-dataDone
	pw.Close()
	out := <-outCh

	if opErr != nil {
		return string(out), fmt.Errorf("exec wait %s: %w", name, opErr)
	}
	return string(out), nil
}

// StreamCommand runs a command in a non-interactive session and returns its stdout as a
// streaming io.ReadCloser. The caller must close the reader when done.
func (c *Client) StreamCommand(name string, command []string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()

	req := incusapi.InstanceExecPost{
		Command:     command,
		WaitForWS:   true,
		Interactive: false,
		Environment: map[string]string{},
	}

	dataDone := make(chan bool)
	args := &incusclient.InstanceExecArgs{
		Stdin:    io.NopCloser(bytes.NewReader(nil)),
		Stdout:   pw,
		Stderr:   pw, // discard stderr by sending to same pipe (acceptable for streaming use)
		DataDone: dataDone,
	}

	op, err := c.server.ExecInstance(name, req, args)
	if err != nil {
		pr.Close()
		pw.Close()
		return nil, fmt.Errorf("stream exec %s: %w", name, err)
	}

	// Close the write side when the operation finishes.
	go func() {
		op.Wait()
		<-dataDone
		pw.Close()
	}()

	return pr, nil
}

func (c *Client) PushFile(instanceName, remotePath string, content []byte, uid, gid int64, mode int) error {
	args := incusclient.InstanceFileArgs{
		Content: bytes.NewReader(content),
		UID:     uid,
		GID:     gid,
		Mode:    mode,
		Type:    "file",
	}
	return c.server.CreateInstanceFile(instanceName, remotePath, args)
}

func (c *Client) GetServerResources() (*incusapi.Resources, error) {
	resources, err := c.server.GetServerResources()
	if err != nil {
		return nil, fmt.Errorf("get server resources: %w", err)
	}
	return resources, nil
}

func (c *Client) GetProfileNames() ([]string, error) {
	names, err := c.server.GetProfileNames()
	if err != nil {
		return nil, fmt.Errorf("get profile names: %w", err)
	}
	return names, nil
}

// ListDirectory lists files/directories at the given path inside the instance.
// Uses find with -printf to get type, size, mtime, and name in a single exec call.
func (c *Client) ListDirectory(instanceName, path string) ([]FileEntry, error) {
	out, err := c.RunCommand(instanceName, []string{
		"/bin/sh", "-c",
		fmt.Sprintf(`find %q -maxdepth 1 -mindepth 1 -printf '%%y %%s %%T@ %%f\n' 2>/dev/null | sort -t' ' -k4`, path),
	})
	if err != nil {
		return nil, fmt.Errorf("list directory %s: %w", path, err)
	}

	var entries []FileEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// Format: "d 4096 1712345678.123 dirname" or "f 1234 1712345678.123 filename"
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 4 {
			continue
		}
		typ := "file"
		if parts[0] == "d" {
			typ = "folder"
		}
		size, _ := strconv.ParseInt(parts[1], 10, 64)
		tsFloat, _ := strconv.ParseFloat(parts[2], 64)
		date := int64(tsFloat)
		name := parts[3]

		entries = append(entries, FileEntry{
			ID:   filepath.Join(path, name),
			Name: name,
			Type: typ,
			Size: size,
			Date: date,
		})
	}
	return entries, nil
}

// GetFile downloads a single file from the instance using the Incus file API.
func (c *Client) GetFile(instanceName, path string) (io.ReadCloser, *FileInfo, error) {
	reader, resp, err := c.server.GetInstanceFile(instanceName, path)
	if err != nil {
		return nil, nil, fmt.Errorf("get file %s: %w", path, err)
	}
	info := &FileInfo{
		UID:  resp.UID,
		GID:  resp.GID,
		Mode: resp.Mode,
		Type: resp.Type,
	}
	return reader, info, nil
}

// StreamDirectory streams a directory as a tar archive.
func (c *Client) StreamDirectory(instanceName, path string) (io.ReadCloser, error) {
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	return c.StreamCommand(instanceName, []string{"tar", "-C", parent, "-cf", "-", base})
}

// ListVolumeSnapshots returns all snapshots for a custom storage volume.
func (c *Client) ListVolumeSnapshots(pool, volumeName string) ([]VolumeSnapshotInfo, error) {
	snapshots, err := c.server.GetStoragePoolVolumeSnapshots(pool, "custom", volumeName)
	if err != nil {
		return nil, fmt.Errorf("list volume snapshots %s/%s: %w", pool, volumeName, err)
	}
	var result []VolumeSnapshotInfo
	for _, s := range snapshots {
		result = append(result, VolumeSnapshotInfo{
			Name:      s.Name,
			CreatedAt: s.CreatedAt,
		})
	}
	return result, nil
}

// CreateVolumeSnapshot creates a named snapshot of a custom storage volume.
func (c *Client) CreateVolumeSnapshot(pool, volumeName, snapshotName string) error {
	req := incusapi.StorageVolumeSnapshotsPost{
		Name: snapshotName,
	}
	op, err := c.server.CreateStoragePoolVolumeSnapshot(pool, "custom", volumeName, req)
	if err != nil {
		return fmt.Errorf("create volume snapshot %s/%s/%s: %w", pool, volumeName, snapshotName, err)
	}
	return op.Wait()
}

// DeleteVolumeSnapshot deletes a named snapshot of a custom storage volume.
func (c *Client) DeleteVolumeSnapshot(pool, volumeName, snapshotName string) error {
	op, err := c.server.DeleteStoragePoolVolumeSnapshot(pool, "custom", volumeName, snapshotName)
	if err != nil {
		return fmt.Errorf("delete volume snapshot %s/%s/%s: %w", pool, volumeName, snapshotName, err)
	}
	return op.Wait()
}

// RestoreVolumeSnapshot restores a custom storage volume to a named snapshot.
func (c *Client) RestoreVolumeSnapshot(pool, volumeName, snapshotName string) error {
	vol, etag, err := c.server.GetStoragePoolVolume(pool, "custom", volumeName)
	if err != nil {
		return fmt.Errorf("get volume for restore %s/%s: %w", pool, volumeName, err)
	}
	writable := vol.Writable()
	writable.Restore = snapshotName
	err = c.server.UpdateStoragePoolVolume(pool, "custom", volumeName, writable, etag)
	if err != nil {
		return fmt.Errorf("restore volume snapshot %s/%s/%s: %w", pool, volumeName, snapshotName, err)
	}
	return nil
}
