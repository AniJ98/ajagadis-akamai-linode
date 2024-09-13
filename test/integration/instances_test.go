package integration

import (
	"context"
	"encoding/base64"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/linode/linodego"
)

type instanceModifier func(*linodego.Client, *linodego.InstanceCreateOptions)

func TestInstances_List(t *testing.T) {
	client, instance, _, teardown, err := setupInstanceWithoutDisks(
		t,
		"fixtures/TestInstances_List", true,
		func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
			options.Region = "eu-west" // Override for metadata availability
		},
	)

	defer teardown()

	if err != nil {
		t.Error(err)
	}

	listOpts := linodego.NewListOptions(1, "{\"id\": "+strconv.Itoa(instance.ID)+"}")
	linodes, err := client.ListInstances(context.Background(), listOpts)
	if err != nil {
		t.Errorf("Error listing instances, expected struct, got error %v", err)
	}
	if len(linodes) != 1 {
		t.Errorf("Expected a list of instances, but got %v", linodes)
	}

	if linodes[0].ID != instance.ID {
		t.Errorf("Expected list of instances to include test instance, but got %v", linodes)
	}

	if linodes[0].HostUUID == "" {
		t.Errorf("failed to get instance HostUUID")
	}

	if linodes[0].HasUserData {
		t.Errorf("expected instance.HasUserData to be false, got true")
	}

	if linodes[0].Specs.GPUs < 0 {
		t.Errorf("failed to retrieve number of GPUs")
	}
}

func TestInstance_Get_smoke(t *testing.T) {
	client, instance, _, teardown, err := setupInstanceWithoutDisks(t, "fixtures/TestInstance_Get", true)
	defer teardown()
	if err != nil {
		t.Error(err)
	}

	instanceGot, err := client.GetInstance(context.Background(), instance.ID)
	if err != nil {
		t.Errorf("Error getting instance: %s", err)
	}
	if instanceGot.ID != instance.ID {
		t.Errorf("Expected instance ID %d to match %d", instanceGot.ID, instance.ID)
	}

	if instance.Specs.Disk <= 0 {
		t.Errorf("Error parsing instance spec for disk size: %v", instance.Specs)
	}

	if instance.HostUUID == "" {
		t.Errorf("failed to get instance HostUUID")
	}

	assertDateSet(t, instance.Created)
	assertDateSet(t, instance.Updated)
}

func TestInstance_Resize(t *testing.T) {
	client, instance, teardown, err := setupInstance(
		t,
		"fixtures/TestInstance_Resize", true,
		func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
			boot := true
			options.Type = "g6-nanode-1"
			options.Booted = &boot
		},
	)

	defer teardown()
	if err != nil {
		t.Error(err)
	}

	instance, err = client.WaitForInstanceStatus(
		context.Background(),
		instance.ID,
		linodego.InstanceRunning,
		180,
	)
	if err != nil {
		t.Errorf("Error waiting for instance readiness for resize: %s", err.Error())
	}

	err = client.ResizeInstance(
		context.Background(),
		instance.ID,
		linodego.InstanceResizeOptions{
			Type:          "g6-standard-1",
			MigrationType: "warm",
		},
	)
	if err != nil {
		t.Errorf("failed to resize instance %d: %v", instance.ID, err.Error())
	}
}

func TestInstance_Disks_List(t *testing.T) {
	client, instance, teardown, err := setupInstance(t, "fixtures/TestInstance_Disks_List", true)
	defer teardown()
	if err != nil {
		t.Error(err)
	}

	disks, err := client.ListInstanceDisks(context.Background(), instance.ID, nil)
	if err != nil {
		t.Errorf("Error listing instance disks, expected struct, got error %v", err)
	}
	if len(disks) == 0 {
		t.Errorf("Expected a list of instance disks, but got %v", disks)
	}
}

func TestInstance_Disks_List_WithEncryption(t *testing.T) {
	client, instance, teardown, err := setupInstance(t, "fixtures/TestInstance_Disks_List_WithEncryption", true, func(c *linodego.Client, ico *linodego.InstanceCreateOptions) {
		ico.Region = getRegionsWithCaps(t, c, []string{"Disk Encryption"})[0]
	})
	defer teardown()
	if err != nil {
		t.Error(err)
	}

	disks, err := client.ListInstanceDisks(context.Background(), instance.ID, nil)
	if err != nil {
		t.Errorf("Error listing instance disks, expected struct, got error %v", err)
	}
	if len(disks) == 0 {
		t.Errorf("Expected a list of instance disks, but got %v", disks)
	}

	// Disk Encryption should be enabled by default if not otherwise specified
	for _, disk := range disks {
		if disk.DiskEncryption != linodego.InstanceDiskEncryptionEnabled {
			t.Fatalf("expected disk encryption status: %s, got :%s", linodego.InstanceDiskEncryptionEnabled, disk.DiskEncryption)
		}
	}
}

func TestInstance_Disk_Resize(t *testing.T) {
	client, instance, _, teardown, err := setupInstanceWithoutDisks(t, "fixtures/TestInstance_Disk_Resize", true)
	defer teardown()
	if err != nil {
		t.Error(err)
	}

	instance, err = client.WaitForInstanceStatus(context.Background(), instance.ID, linodego.InstanceOffline, 180)
	if err != nil {
		t.Errorf("Error waiting for instance readiness for resize: %s", err)
	}

	disk, err := client.CreateInstanceDisk(context.Background(), instance.ID, linodego.InstanceDiskCreateOptions{
		Label:      "disk-test-" + randLabel(),
		Filesystem: "ext4",
		Size:       2000,
	})
	if err != nil {
		t.Errorf("Error creating disk for resize: %s", err)
	}

	disk, err = client.WaitForInstanceDiskStatus(context.Background(), instance.ID, disk.ID, linodego.DiskReady, 180)
	if err != nil {
		t.Errorf("Error waiting for disk readiness for resize: %s", err)
	}

	err = client.ResizeInstanceDisk(context.Background(), instance.ID, disk.ID, 4000)
	if err != nil {
		t.Errorf("Error resizing instance disk: %s", err)
	}
}

func TestInstance_Disk_ListMultiple(t *testing.T) {
	// This is a long running test
	client, instance1, teardown1, err := setupInstance(t, "fixtures/TestInstance_Disk_ListMultiple_Primary", true)
	defer teardown1()
	if err != nil {
		t.Error(err)
	}
	err = client.BootInstance(context.Background(), instance1.ID, 0)
	if err != nil {
		t.Error(err)
	}
	instance1, err = client.WaitForInstanceStatus(context.Background(), instance1.ID, linodego.InstanceRunning, 180)
	if err != nil {
		t.Errorf("Error waiting for instance readiness: %s", err)
	}

	disks, err := client.ListInstanceDisks(context.Background(), instance1.ID, nil)
	if err != nil {
		t.Error(err)
	}

	disk, err := client.WaitForInstanceDiskStatus(context.Background(), instance1.ID, disks[0].ID, linodego.DiskReady, 180)
	if err != nil {
		t.Errorf("Error waiting for disk readiness: %s", err)
	}

	imageLabel := "go-test-image-" + randLabel()
	imageCreateOptions := linodego.ImageCreateOptions{Label: imageLabel, DiskID: disk.ID}
	image, err := client.CreateImage(context.Background(), imageCreateOptions)

	defer client.DeleteImage(context.Background(), image.ID)
	if err != nil {
		t.Error(err)
	}

	client, instance2, _, teardown2, err := setupInstanceWithoutDisks(t, "fixtures/TestInstance_Disk_ListMultiple_Secondary", true)
	defer teardown2()
	if err != nil {
		t.Error(err)
	}
	instance2, err = client.WaitForInstanceStatus(context.Background(), instance2.ID, linodego.InstanceOffline, 180)
	if err != nil {
		t.Errorf("Error waiting for instance readiness: %s", err)
	}

	_, err = client.WaitForEventFinished(context.Background(), instance1.ID, linodego.EntityLinode, linodego.ActionDiskImagize, *disk.Created, 300)
	if err != nil {
		t.Errorf("Error waiting for imagize event: %s", err)
	}

	_, err = client.CreateInstanceDisk(context.Background(), instance2.ID, linodego.InstanceDiskCreateOptions{
		Label:    "go-disk-test-" + randLabel(),
		Image:    image.ID,
		RootPass: randPassword(),
		Size:     2000,
	})
	if err != nil {
		t.Errorf("Error creating disk from private image: %s", err)
	}

	_, err = client.CreateInstanceDisk(context.Background(), instance2.ID, linodego.InstanceDiskCreateOptions{
		Label: "go-disk-test-" + randLabel(),
		Size:  2000,
	})
	if err != nil {
		t.Errorf("Error creating disk after a private image: %s", err)
	}

	disks, err = client.ListInstanceDisks(context.Background(), instance2.ID, nil)
	if err != nil {
		t.Errorf("Error listing instance disks, expected struct, got error %v", err)
	}
	if len(disks) != 2 {
		t.Errorf("Expected a list of instance disks, but got %v", disks)
	}
}

func TestInstance_Disk_ResetPassword(t *testing.T) {
	client, instance, _, teardown, err := setupInstanceWithoutDisks(t, "fixtures/TestInstance_Disk_ResetPassword", true)
	defer teardown()
	if err != nil {
		t.Error(err)
	}

	instance, err = client.WaitForInstanceStatus(context.Background(), instance.ID, linodego.InstanceOffline, 180)
	if err != nil {
		t.Errorf("Error waiting for instance readiness for password reset: %s", err)
	}

	disk, err := client.CreateInstanceDisk(context.Background(), instance.ID, linodego.InstanceDiskCreateOptions{
		Label:      "go-disk-test-" + randLabel(),
		Filesystem: "ext4",
		Image:      "linode/debian9",
		RootPass:   randPassword(),
		Size:       2000,
	})
	if err != nil {
		t.Errorf("Error creating disk for password reset: %s", err)
	}

	instance, err = client.WaitForInstanceStatus(context.Background(), instance.ID, linodego.InstanceOffline, 180)
	if err != nil {
		t.Errorf("Error waiting for instance readiness after creating disk for password reset: %s", err)
	}
	disk, err = client.WaitForInstanceDiskStatus(context.Background(), instance.ID, disk.ID, linodego.DiskReady, 180)
	if err != nil {
		t.Errorf("Error waiting for disk readiness for password reset: %s", err)
	}

	err = client.PasswordResetInstanceDisk(context.Background(), instance.ID, disk.ID, "r34!_b4d_p455")
	if err != nil {
		t.Errorf("Error reseting password on instance disk: %s", err)
	}
}

func TestInstance_Volumes_List(t *testing.T) {
	client, instance, config, teardown, err := setupInstanceWithoutDisks(t, "fixtures/TestInstance_Volumes_List_Instance", true)
	defer teardown()
	if err != nil {
		t.Error(err)
	}

	volume, teardown, volErr := createVolume(t, client)

	_, err = client.WaitForVolumeStatus(context.Background(), volume.ID, linodego.VolumeActive, 500)
	if err != nil {
		t.Errorf("Error waiting for volume to be active, %s", err)
	}

	defer teardown()
	if volErr != nil {
		t.Error(err)
	}

	configOpts := linodego.InstanceConfigUpdateOptions{
		Label: "go-vol-test" + getUniqueText(),
		Devices: &linodego.InstanceConfigDeviceMap{
			SDA: &linodego.InstanceConfigDevice{
				VolumeID: volume.ID,
			},
		},
	}
	_, err = client.UpdateInstanceConfig(context.Background(), instance.ID, config.ID, configOpts)
	if err != nil {
		t.Error(err)
	}

	volumes, err := client.ListInstanceVolumes(context.Background(), instance.ID, nil)
	if err != nil {
		t.Errorf("Error listing instance volumes, expected struct, got error %v", err)
	}
	if len(volumes) == 0 {
		t.Errorf("Expected an list of instance volumes, but got %v", volumes)
	}
}

func TestInstance_CreateUnderFirewall(t *testing.T) {
	client, firewall, firewallTeardown, err := setupFirewall(
		t,
		[]firewallModifier{},
		"fixtures/TestInstance_CreateUnderFirewall",
	)
	defer firewallTeardown()

	if err != nil {
		t.Error(err)
	}
	_, _, teardownInstance, err := createInstanceWithoutDisks(
		t,
		client, true,
		func(_ *linodego.Client, options *linodego.InstanceCreateOptions) {
			options.FirewallID = firewall.ID
		},
	)
	defer teardownInstance()

	if err != nil {
		t.Error(err)
	}
}

func TestInstance_Rebuild(t *testing.T) {
	client, instance, _, teardown, err := setupInstanceWithoutDisks(
		t,
		"fixtures/TestInstance_Rebuild", true,
		func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
			options.Region = getRegionsWithCaps(t, client, []string{"Metadata"})[0]
		},
	)
	defer teardown()

	if err != nil {
		t.Error(err)
	}

	_, err = client.WaitForEventFinished(context.Background(), instance.ID, linodego.EntityLinode, linodego.ActionLinodeCreate, *instance.Created, 180)
	if err != nil {
		t.Errorf("Error waiting for instance created: %s", err)
	}

	rebuildOpts := linodego.InstanceRebuildOptions{
		Image: "linode/alpine3.19",
		Metadata: &linodego.InstanceMetadataOptions{
			UserData: base64.StdEncoding.EncodeToString([]byte("cool")),
		},
		RootPass: randPassword(),
		Type:     "g6-standard-2",
	}
	instance, err = client.RebuildInstance(context.Background(), instance.ID, rebuildOpts)
	if err != nil {
		t.Fatal(err)
	}

	if !instance.HasUserData {
		t.Fatal("expected instance.HasUserData to be true, got false")
	}
}

func TestInstance_RebuildWithEncryption(t *testing.T) {
	client, instance, _, teardown, err := setupInstanceWithoutDisks(
		t,
		"fixtures/TestInstance_RebuildWithEncryption",
		true,
		func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
			options.Region = getRegionsWithCaps(t, client, []string{"Disk Encryption"})[0]
			options.DiskEncryption = linodego.InstanceDiskEncryptionEnabled
		},
	)
	defer teardown()

	if err != nil {
		t.Error(err)
	}

	_, err = client.WaitForEventFinished(context.Background(), instance.ID, linodego.EntityLinode, linodego.ActionLinodeCreate, *instance.Created, 180)
	if err != nil {
		t.Errorf("Error waiting for instance created: %s", err)
	}

	rebuildOpts := linodego.InstanceRebuildOptions{
		Image:          "linode/alpine3.19",
		RootPass:       randPassword(),
		Type:           "g6-standard-2",
		DiskEncryption: linodego.InstanceDiskEncryptionDisabled,
	}
	instance, err = client.RebuildInstance(context.Background(), instance.ID, rebuildOpts)
	if err != nil {
		t.Fatal(err)
	}

	if instance.DiskEncryption != linodego.InstanceDiskEncryptionDisabled {
		t.Fatalf("expected instance.DiskEncryption to be: %s, got: %s", linodego.InstanceDiskEncryptionDisabled, linodego.InstanceDiskEncryptionEnabled)
	}
}

func TestInstance_Clone(t *testing.T) {
	var targetRegion string

	client, instance, teardownOriginalLinode, err := setupInstance(
		t, "fixtures/TestInstance_Clone", true,
		func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
			targetRegion = getRegionsWithCaps(t, client, []string{"Metadata"})[0]

			options.Region = targetRegion
		})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(teardownOriginalLinode)

	_, err = client.WaitForEventFinished(
		context.Background(),
		instance.ID,
		linodego.EntityLinode,
		linodego.ActionLinodeCreate,
		*instance.Created,
		180,
	)
	if err != nil {
		t.Errorf("Error waiting for instance created: %s", err)
	}

	cloneOpts := linodego.InstanceCloneOptions{
		Region:    targetRegion,
		Type:      "g6-nanode-1",
		PrivateIP: true,
		Metadata: &linodego.InstanceMetadataOptions{
			UserData: base64.StdEncoding.EncodeToString([]byte("reallycooluserdata")),
		},
	}
	clonedInstance, err := client.CloneInstance(context.Background(), instance.ID, cloneOpts)

	t.Cleanup(func() {
		client.DeleteInstance(context.Background(), clonedInstance.ID)
	})

	if err != nil {
		t.Error(err)
	}

	_, err = client.WaitForEventFinished(
		context.Background(),
		instance.ID,
		linodego.EntityLinode,
		linodego.ActionLinodeClone,
		*clonedInstance.Created,
		240,
	)
	if err != nil {
		t.Fatal(err)
	}

	if clonedInstance.Image != instance.Image {
		t.Fatal("Clone instance image mismatched.")
	}

	clonedInstanceIPs, err := client.GetInstanceIPAddresses(
		context.Background(),
		clonedInstance.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(clonedInstanceIPs.IPv4.Private) == 0 {
		t.Fatal("No private IPv4 assigned to the cloned instance.")
	}

	if !clonedInstance.HasUserData {
		t.Fatal("expected instance.HasUserData to be true, got false")
	}
}

func TestInstance_withMetadata(t *testing.T) {
	_, inst, _, teardown, err := setupInstanceWithoutDisks(t, "fixtures/TestInstance_withMetadata", true,
		func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
			options.Metadata = &linodego.InstanceMetadataOptions{
				UserData: base64.StdEncoding.EncodeToString([]byte("reallycoolmetadata")),
			}
			options.Region = getRegionsWithCaps(t, client, []string{"Metadata"})[0]
		})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(teardown)

	if !inst.HasUserData {
		t.Fatalf("expected instance.HasUserData to be true, got false")
	}
}

func TestInstance_DiskEncryption(t *testing.T) {
	_, inst, teardown, err := setupInstance(t, "fixtures/TestInstance_DiskEncryption", true, func(c *linodego.Client, ico *linodego.InstanceCreateOptions) {
		ico.DiskEncryption = linodego.InstanceDiskEncryptionEnabled
		ico.Region = "us-east"
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(teardown)

	if inst.DiskEncryption != linodego.InstanceDiskEncryptionEnabled {
		t.Fatalf("expected instance to have disk encryption enabled, got: %s, want: %s", inst.DiskEncryption, linodego.InstanceDiskEncryptionEnabled)
	}
}

func TestInstance_withPG(t *testing.T) {
	client, clientTeardown := createTestClient(t, "fixtures/TestInstance_withPG")

	pg, pgTeardown, err := createPlacementGroup(t, client)
	require.NoError(t, err)

	// Create an instance to assign to the PG
	inst, err := createInstance(t, client, true, func(client *linodego.Client, options *linodego.InstanceCreateOptions) {
		options.Region = pg.Region
		options.PlacementGroup = &linodego.InstanceCreatePlacementGroupOptions{
			ID: pg.ID,
		}
	})
	require.NoError(t, err)

	defer func() {
		client.DeleteInstance(context.Background(), inst.ID)
		pgTeardown()
		clientTeardown()
	}()

	require.NotNil(t, inst.PlacementGroup)
	require.Equal(t, inst.PlacementGroup.ID, pg.ID)
	require.Equal(t, inst.PlacementGroup.Label, pg.Label)
	require.Equal(t, inst.PlacementGroup.PlacementGroupType, pg.PlacementGroupType)
	require.Equal(t, inst.PlacementGroup.PlacementGroupPolicy, pg.PlacementGroupPolicy)
}

func TestInstance_CreateWithReservedIPAddress(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithReservedIPAddress")
	defer teardown()

	// Reserve an IP for testing
	reservedIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{Region: "us-east"})
	if err != nil {
		t.Fatalf("Failed to reserve IP: %v", err)
	}
	defer func() {
		err := client.DeleteReservedIPAddress(context.Background(), reservedIP.Address)
		if err != nil {
			t.Errorf("Failed to delete reserved IP: %v", err)
		}
	}()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, reservedIP.Address)
	if err != nil {
		t.Fatalf("Error creating instance with reserved IP: %s", err)
	}
	defer instanceTeardown()

}

func createInstanceWithReservedIP(
	t *testing.T,
	client *linodego.Client,
	reservedIP string,
	modifiers ...instanceModifier,
) (*linodego.Instance, func(), error) {
	t.Helper()

	createOpts := linodego.InstanceCreateOptions{
		Label:    "go-test-ins-reserved-ip-" + randLabel(),
		Region:   "us-east",
		Type:     "g6-nanode-1",
		Booted:   linodego.Pointer(false),
		Image:    "linode/alpine3.17",
		RootPass: randPassword(),
		Interfaces: []linodego.InstanceConfigInterfaceCreateOptions{
			{
				Purpose:     linodego.InterfacePurposePublic,
				Label:       "",
				IPAMAddress: "",
			},
		},
		Ipv4: []string{reservedIP},
	}

	for _, modifier := range modifiers {
		modifier(client, &createOpts)
	}

	instance, err := client.CreateInstance(context.Background(), createOpts)
	if err != nil {
		return nil, func() {}, err
	}

	teardown := func() {
		if terr := client.DeleteInstance(context.Background(), instance.ID); terr != nil {
			t.Errorf("Error deleting test Instance: %s", terr)
		}
	}

	return instance, teardown, nil
}

func TestInstance_CreateWithOwnedNonAssignedReservedIP(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithOwnedNonAssignedReservedIP")
	defer teardown()

	reservedIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{Region: "us-east"})
	if err != nil {
		t.Fatalf("Failed to reserve IP: %v", err)
	}
	defer func() {
		err := client.DeleteReservedIPAddress(context.Background(), reservedIP.Address)
		if err != nil {
			t.Errorf("Failed to delete reserved IP: %v", err)
		}
	}()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, reservedIP.Address)
	if err != nil {
		t.Errorf("Unexpected error with owned non-assigned reserved IP: %v", err)
	} else {
		instanceTeardown()
	}
}

func TestInstance_CreateWithAlreadyAssignedReservedIP(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithAlreadyAssignedReservedIP")
	defer teardown()

	reservedIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{Region: "us-east"})
	if err != nil {
		t.Fatalf("Failed to reserve IP: %v", err)
	}
	defer func() {
		err := client.DeleteReservedIPAddress(context.Background(), reservedIP.Address)
		if err != nil {
			t.Errorf("Failed to delete reserved IP: %v", err)
		}
	}()

	// First, create an instance with the reserved IP
	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, reservedIP.Address)
	if err != nil {
		t.Fatalf("Failed to create initial instance: %v", err)
	}
	defer instanceTeardown()

	// Now try to create another instance with the same IP
	_, secondInstanceTeardown, err := createInstanceWithReservedIP(t, client, reservedIP.Address)
	if err == nil {
		t.Errorf("Expected error with already assigned reserved IP, but got none")
		secondInstanceTeardown()
	}
}

func TestInstance_CreateWithNonReservedAddress(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithNonReservedAddress")
	defer teardown()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, "192.0.2.1")
	if err == nil {
		t.Errorf("Expected error with non-reserved address, but got none")
		instanceTeardown()
	}
}

func TestInstance_CreateWithNonOwnedReservedAddress(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithNonOwnedReservedAddress")
	defer teardown()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, "198.51.100.1")
	if err == nil {
		t.Errorf("Expected error with non-owned reserved address, but got none")
		instanceTeardown()
	}
}

func TestInstance_CreateWithEmptyIPAddress(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithEmptyIPAddress")
	defer teardown()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, "")
	if err == nil {
		t.Errorf("Expected error with empty IP address, but got none")
		instanceTeardown()
	}
}

func TestInstance_CreateWithNullIPAddress(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithNullIPAddress")
	defer teardown()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, "", func(client *linodego.Client, opts *linodego.InstanceCreateOptions) {
		opts.Ipv4 = nil
	})
	if err != nil {
		t.Errorf("Unexpected error with null IP address: %v", err)
	} else {
		instanceTeardown()
	}
}

func TestInstance_CreateWithMultipleIPAddresses(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithMultipleIPAddresses")
	defer teardown()

	reservedIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{Region: "us-east"})
	if err != nil {
		t.Fatalf("Failed to reserve IP: %v", err)
	}
	defer func() {
		err := client.DeleteReservedIPAddress(context.Background(), reservedIP.Address)
		if err != nil {
			t.Errorf("Failed to delete reserved IP: %v", err)
		}
	}()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, "", func(client *linodego.Client, opts *linodego.InstanceCreateOptions) {
		opts.Ipv4 = []string{reservedIP.Address, "192.0.2.2"}
	})
	if err == nil {
		t.Errorf("Expected error with multiple IP addresses, but got none")
		instanceTeardown()
	}
}

func TestInstance_CreateWithoutIPv4Field(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_CreateWithoutIPv4Field")
	defer teardown()

	_, instanceTeardown, err := createInstanceWithReservedIP(t, client, "", func(client *linodego.Client, opts *linodego.InstanceCreateOptions) {
		opts.Ipv4 = nil
	})
	if err != nil {
		t.Errorf("Unexpected error when omitting IPv4 field: %v", err)
	} else {
		instanceTeardown()
	}
}

func createInstance(t *testing.T, client *linodego.Client, enableCloudFirewall bool, modifiers ...instanceModifier) (*linodego.Instance, error) {
	if t != nil {
		t.Helper()
	}

	createOpts := linodego.InstanceCreateOptions{
		Label:    "go-test-ins-" + randLabel(),
		RootPass: randPassword(),
		Region:   getRegionsWithCaps(t, client, []string{"linodes"})[0],
		Type:     "g6-nanode-1",
		Image:    "linode/debian9",
		Booted:   linodego.Pointer(false),
	}

	if enableCloudFirewall {
		createOpts.FirewallID = firewallID
	}

	for _, modifier := range modifiers {
		modifier(client, &createOpts)
	}
	return client.CreateInstance(context.Background(), createOpts)
}

func setupInstance(t *testing.T, fixturesYaml string, EnableCloudFirewall bool, modifiers ...instanceModifier) (*linodego.Client, *linodego.Instance, func(), error) {
	if t != nil {
		t.Helper()
	}
	client, fixtureTeardown := createTestClient(t, fixturesYaml)

	instance, err := createInstance(t, client, EnableCloudFirewall, modifiers...)
	if err != nil {
		t.Errorf("failed to create test instance: %s", err)
	}

	teardown := func() {
		if err := client.DeleteInstance(context.Background(), instance.ID); err != nil {
			if t != nil {
				t.Errorf("Error deleting test Instance: %s", err)
			}
		}
		fixtureTeardown()
	}
	return client, instance, teardown, err
}

func createInstanceWithoutDisks(
	t *testing.T,
	client *linodego.Client,
	enableCloudFirewall bool,
	modifiers ...instanceModifier,
) (*linodego.Instance, *linodego.InstanceConfig, func(), error) {
	t.Helper()

	createOpts := linodego.InstanceCreateOptions{
		Label:  "go-test-ins-wo-disk-" + randLabel(),
		Region: getRegionsWithCaps(t, client, []string{"linodes"})[0],
		Type:   "g6-nanode-1",
		Booted: linodego.Pointer(false),
	}

	if enableCloudFirewall {
		createOpts.FirewallID = GetFirewallID()
	}

	for _, modifier := range modifiers {
		modifier(client, &createOpts)
	}

	instance, err := client.CreateInstance(context.Background(), createOpts)
	if err != nil {
		t.Errorf("Error creating test Instance: %s", err)
		return nil, nil, func() {}, err
	}
	configOpts := linodego.InstanceConfigCreateOptions{
		Label: "go-test-conf-" + randLabel(),
	}
	config, err := client.CreateInstanceConfig(context.Background(), instance.ID, configOpts)
	if err != nil {
		t.Errorf("Error creating config: %s", err)
		return nil, nil, func() {}, err
	}

	teardown := func() {
		if terr := client.DeleteInstance(context.Background(), instance.ID); terr != nil {
			t.Errorf("Error deleting test Instance: %s", terr)
		}
	}
	return instance, config, teardown, err
}

func setupInstanceWithoutDisks(t *testing.T, fixturesYaml string, enableCloudFirewall bool, modifiers ...instanceModifier) (*linodego.Client, *linodego.Instance, *linodego.InstanceConfig, func(), error) {
	t.Helper()
	client, fixtureTeardown := createTestClient(t, fixturesYaml)
	instance, config, instanceTeardown, err := createInstanceWithoutDisks(t, client, enableCloudFirewall, modifiers...)

	teardown := func() {
		instanceTeardown()
		fixtureTeardown()
	}
	return client, instance, config, teardown, err
}

func TestInstance_AddReservedIPToInstance(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_AddReservedIPToInstance")
	defer teardown()

	// Create a test Linode instance
	instance, err := client.CreateInstance(context.Background(), linodego.InstanceCreateOptions{
		Region:   "us-east",
		Type:     "g6-nanode-1",
		Label:    "test-instance-for-ip-reservation",
		RootPass: randPassword(),
	})
	if err != nil {
		t.Fatalf("Error creating test instance: %v", err)
	}
	defer func() {
		if err := client.DeleteInstance(context.Background(), instance.ID); err != nil {
			t.Errorf("Error deleting test instance: %v", err)
		}
	}()

	// Reserve an IP address
	reservedIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{
		Region: "us-east",
	})
	if err != nil {
		t.Fatalf("Error reserving IP address: %v", err)
	}
	defer func() {
		if err := client.DeleteReservedIPAddress(context.Background(), reservedIP.Address); err != nil {
			t.Errorf("Error deleting reserved IP: %v", err)
		}
	}()

	// Add the reserved IP to the instance
	opts := linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: reservedIP.Address,
	}
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, opts)
	if err != nil {
		t.Fatalf("Error adding reserved IP to instance: %v", err)
	}

	// Verify the IP was added to the instance
	ips, err := client.GetInstanceIPAddresses(context.Background(), instance.ID)
	if err != nil {
		t.Fatalf("Error getting instance IP addresses: %v", err)
	}

	found := false
	for _, ip := range ips.IPv4.Public {
		if ip.Address == reservedIP.Address {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Reserved IP %s was not found in instance's IP addresses", reservedIP.Address)
	}
}

func TestInstance_AddReservedIPToInstanceVariants(t *testing.T) {
	client, teardown := createTestClient(t, "fixtures/TestInstance_AddReservedIPToInstanceVariants")
	defer teardown()

	// Create a test Linode instance
	instance, err := client.CreateInstance(context.Background(), linodego.InstanceCreateOptions{
		Region:   "us-east",
		Type:     "g6-nanode-1",
		Label:    "test-instance-for-ip-reservation",
		RootPass: randPassword(),
	})
	if err != nil {
		t.Fatalf("Error creating test instance: %v", err)
	}
	defer func() {
		if err := client.DeleteInstance(context.Background(), instance.ID); err != nil {
			t.Errorf("Error deleting test instance: %v", err)
		}
	}()

	// Reserve an IP address
	reservedIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{
		Region: "us-east",
	})
	if err != nil {
		t.Fatalf("Error reserving IP address: %v", err)
	}
	defer func() {
		if err := client.DeleteReservedIPAddress(context.Background(), reservedIP.Address); err != nil {
			t.Errorf("Error deleting reserved IP: %v", err)
		}
	}()

	// Test: Add reserved IP to instance with valid parameters
	opts := linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: reservedIP.Address,
	}
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, opts)
	if err != nil {
		t.Fatalf("Error adding reserved IP to instance: %v", err)
	}

	// Test: Omit public field
	omitPublicOpts := linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Address: reservedIP.Address,
		// Public field is omitted here
	}
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, omitPublicOpts)
	if err == nil {
		t.Fatalf("Expected error when adding reserved IP with omitted public field, but got none")
	}

	// Assume we have a Linode that has been created without a reserved IP address and IPMAX set to 1
	linodeID := 63510870

	// Reserve IP address
	resIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{
		Region: "us-east",
	})
	if err != nil {
		t.Fatalf("Failed to reserve IP: %v", err)
	}

	//  Add IP address to the Linode
	_, err = client.AddReservedIPToInstance(context.Background(), linodeID, linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: resIP.Address,
	})
	if err == nil {
		t.Errorf("Expected error when adding reserved IP to a Linode at its IPMAX limit, but got none")
	}

	// Delete the reserved IP Address

	if err := client.DeleteReservedIPAddress(context.Background(), resIP.Address); err != nil {
		t.Errorf("Failed to delete first reserved IP: %v", err)
	}

	// Test: Non-owned Linode ID
	nonOwnedInstanceID := 888888 // Replace with an actual non-owned Linode ID
	_, err = client.AddReservedIPToInstance(context.Background(), nonOwnedInstanceID, opts)
	if err == nil {
		t.Errorf("Expected error when adding reserved IP to non-owned Linode, but got none")
	}

	// Test: Already assigned reserved IP
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, opts)
	if err == nil {
		t.Errorf("Expected error when adding already assigned reserved IP, but got none")
	}

	// Test: Non-owned reserved IP
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: "198.51.100.1", // Assume this is a non-owned reserved IP
	})
	if err == nil {
		t.Errorf("Expected error when adding non-owned reserved IP, but got none")
	}

	// Test: Reserved IP in different datacenter
	// Reserve an IP address
	diffDataCentreIP, err := client.ReserveIPAddress(context.Background(), linodego.ReserveIPOptions{
		Region: "ca-central",
	})
	if err != nil {
		t.Fatalf("Error reserving IP address: %v", err)
	}
	defer func() {
		if err := client.DeleteReservedIPAddress(context.Background(), diffDataCentreIP.Address); err != nil {
			t.Errorf("Error deleting reserved IP: %v", err)
		}
	}()
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: diffDataCentreIP.Address, // Assume this IP is in a different datacenter
	})
	if err == nil {
		t.Errorf("Expected error when adding reserved IP in different datacenter, but got none")
	}

	// Test: IPv6 type
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:    "ipv6",
		Public:  true,
		Address: reservedIP.Address,
	})
	if err == nil {
		t.Errorf("Expected error when adding reserved IP with type ipv6, but got none")
	}

	// Test: Public field set to false
	opts.Public = false
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, opts)
	if err == nil {
		t.Errorf("Expected error when adding reserved IP with public field set to false, but got none")
	}

	// Test: Integer as address
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: "12345", // Invalid IP format
	})
	if err == nil {
		t.Errorf("Expected error when adding reserved IP with integer as address, but got none")
	}

	// Test: Empty address
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:    "ipv4",
		Public:  true,
		Address: "",
	})
	if err == nil {
		t.Errorf("Expected error when adding reserved IP with empty address, but got none")
	}

	// Test: Null address
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:   "ipv4",
		Public: true,
	})
	if err == nil {
		t.Errorf("Expected error when adding reserved IP with null address, but got none")
	}

	// Test: Omit address field
	_, err = client.AddReservedIPToInstance(context.Background(), instance.ID, linodego.InstanceReserveIPOptions{
		Type:   "ipv4",
		Public: true,
	})
	if err == nil {
		t.Errorf("Expected error when omitting address field, but got none")
	}
}
