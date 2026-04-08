package services

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

type StorageService struct {
	db   *sqlx.DB
	pool *incus.Pool
}

func NewStorageService(db *sqlx.DB, pool *incus.Pool) *StorageService {
	return &StorageService{db: db, pool: pool}
}

// getClientForInstance validates ownership and returns the Incus client + instance record.
func (s *StorageService) getClientForInstance(instanceID, userID int64) (*models.Instance, incus.IncusClient, error) {
	inst, err := queries.GetInstanceByUser(s.db, instanceID, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("instance not found: %w", err)
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("get incus client: %w", err)
	}
	return inst, client, nil
}

// getVolumeForInstance validates that the volume belongs to the instance.
func (s *StorageService) getVolumeForInstance(instanceID, volumeID int64) (*models.Volume, error) {
	vols, err := queries.ListInstanceVolumes(s.db, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list instance volumes: %w", err)
	}
	for _, iv := range vols {
		if iv.VolumeID == volumeID {
			vol, err := queries.GetVolume(s.db, volumeID)
			if err != nil {
				return nil, fmt.Errorf("get volume: %w", err)
			}
			return vol, nil
		}
	}
	return nil, fmt.Errorf("volume %d not attached to instance %d", volumeID, instanceID)
}

// isPathWithinVolumes checks that the path is within one of the instance's mount paths.
func isPathWithinVolumes(path string, vols []models.InstanceVolume) bool {
	cleaned := filepath.Clean(path)
	for _, v := range vols {
		mount := filepath.Clean(v.MountPath)
		if cleaned == mount || strings.HasPrefix(cleaned, mount+"/") {
			return true
		}
	}
	return false
}

// ListDirectory lists directory contents at the given path inside the instance.
func (s *StorageService) ListDirectory(instanceID, userID int64, path string) ([]incus.FileEntry, error) {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return nil, err
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance must be running to browse files")
	}

	// Validate path is within a volume mount
	ivols, _ := queries.ListInstanceVolumes(s.db, inst.ID)
	if !isPathWithinVolumes(path, ivols) {
		return nil, fmt.Errorf("path %q is not within a mounted volume", path)
	}

	return client.ListDirectory(inst.IncusName, path)
}

// DownloadFile streams a single file from the instance.
// Returns the reader, the basename for Content-Disposition, and any error.
func (s *StorageService) DownloadFile(instanceID, userID int64, path string) (io.ReadCloser, string, error) {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return nil, "", err
	}
	if inst.Status != "running" {
		return nil, "", fmt.Errorf("instance must be running to download files")
	}

	ivols, _ := queries.ListInstanceVolumes(s.db, inst.ID)
	if !isPathWithinVolumes(path, ivols) {
		return nil, "", fmt.Errorf("path %q is not within a mounted volume", path)
	}

	reader, info, err := client.GetFile(inst.IncusName, path)
	if err != nil {
		return nil, "", err
	}
	if info.Type == "directory" {
		if reader != nil {
			reader.Close()
		}
		return nil, "", fmt.Errorf("path is a directory, use download-dir instead")
	}

	return reader, filepath.Base(path), nil
}

// DownloadDirectory streams a directory as a tar archive.
func (s *StorageService) DownloadDirectory(instanceID, userID int64, path string) (io.ReadCloser, string, error) {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return nil, "", err
	}
	if inst.Status != "running" {
		return nil, "", fmt.Errorf("instance must be running to download directories")
	}

	ivols, _ := queries.ListInstanceVolumes(s.db, inst.ID)
	if !isPathWithinVolumes(path, ivols) {
		return nil, "", fmt.Errorf("path %q is not within a mounted volume", path)
	}

	reader, err := client.StreamDirectory(inst.IncusName, path)
	if err != nil {
		return nil, "", err
	}

	return reader, filepath.Base(path) + ".tar", nil
}

// ListSnapshots lists snapshots for a volume belonging to the user's instance.
func (s *StorageService) ListSnapshots(instanceID, userID int64, volumeID int64) ([]incus.VolumeSnapshotInfo, error) {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return nil, err
	}
	_ = inst

	vol, err := s.getVolumeForInstance(instanceID, volumeID)
	if err != nil {
		return nil, err
	}

	return client.ListVolumeSnapshots(vol.Pool, vol.Name)
}

// CreateSnapshot creates a named snapshot for a volume.
func (s *StorageService) CreateSnapshot(instanceID, userID int64, volumeID int64, name string) error {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return err
	}
	_ = inst

	vol, err := s.getVolumeForInstance(instanceID, volumeID)
	if err != nil {
		return err
	}

	return client.CreateVolumeSnapshot(vol.Pool, vol.Name, name)
}

// DeleteSnapshot deletes a named volume snapshot.
func (s *StorageService) DeleteSnapshot(instanceID, userID int64, volumeID int64, snapshotName string) error {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return err
	}
	_ = inst

	vol, err := s.getVolumeForInstance(instanceID, volumeID)
	if err != nil {
		return err
	}

	return client.DeleteVolumeSnapshot(vol.Pool, vol.Name, snapshotName)
}

// RestoreSnapshot restores a volume from a named snapshot.
// The instance must be stopped for data consistency.
func (s *StorageService) RestoreSnapshot(instanceID, userID int64, volumeID int64, snapshotName string) error {
	inst, client, err := s.getClientForInstance(instanceID, userID)
	if err != nil {
		return err
	}
	if inst.Status == "running" {
		return fmt.Errorf("instance must be stopped before restoring a snapshot")
	}

	vol, err := s.getVolumeForInstance(instanceID, volumeID)
	if err != nil {
		return err
	}

	return client.RestoreVolumeSnapshot(vol.Pool, vol.Name, snapshotName)
}
