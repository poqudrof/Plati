package incus

import "fmt"

// CreateAndAttachVolume creates a storage volume and attaches it to an instance at the given path.
func CreateAndAttachVolume(client IncusClient, pool, volumeName, instanceName, path string, sizeGB int) error {
	if err := client.CreateVolume(pool, volumeName, sizeGB); err != nil {
		return fmt.Errorf("create volume %s: %w", volumeName, err)
	}

	if err := client.AttachVolume(pool, volumeName, instanceName, "workspace", path); err != nil {
		// Clean up volume if attach fails
		_ = client.DeleteVolume(pool, volumeName)
		return fmt.Errorf("attach volume %s to %s: %w", volumeName, instanceName, err)
	}

	return nil
}

// DetachAndDeleteVolume detaches and removes a storage volume.
func DetachAndDeleteVolume(client IncusClient, pool, volumeName, instanceName string) error {
	if err := client.DetachVolume(instanceName, "workspace"); err != nil {
		return fmt.Errorf("detach volume %s: %w", volumeName, err)
	}
	if err := client.DeleteVolume(pool, volumeName); err != nil {
		return fmt.Errorf("delete volume %s: %w", volumeName, err)
	}
	return nil
}
