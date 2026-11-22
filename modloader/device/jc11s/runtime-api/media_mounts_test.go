package main

import "testing"

func TestInternalDrivePathClassification(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"JP24 SATA partition", "/sys/devices/platform/fe210000.sata/ata1/host1/target1:0:0/1:0:0:0/block/sda/sda1", true},
		{"legacy ATA controller", "/sys/devices/pci0000:00/ata1/host1/block/sda/sda1", true},
		{"NVMe", "/sys/devices/platform/nvme0/nvme0n1/nvme0n1p1", true},
		{"USB partition", "/sys/devices/platform/fc800000.usb/usb3/3-1/3-1.3/3-1.3:1.0/host2/target2:0:0/2:0:0:0/block/sdc/sdc2", false},
		{"JP24 SD slot partition", "/sys/devices/platform/fc800000.usb/usb3/3-1/3-1.4/3-1.4:1.0/host0/target0:0:0/0:0:0:0/block/sdb/sdb1", false},
		{"MMC", "/sys/devices/platform/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p12", false},
		{"unknown bus", "/sys/devices/platform/unknown/block/sda/sda1", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := isInternalDrivePath(test.path); got != test.want {
				t.Fatalf("isInternalDrivePath(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestRemovableSourceClassification(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"USB A/C storage", "/sys/devices/platform/fc800000.usb/usb3/3-1/host2/block/sdc/sdc2", true},
		{"JP24 SD slot USB mass storage", "/sys/devices/platform/fc800000.usb/usb3/3-1/3-1.4/3-1.4:1.0/host0/target0:0:0/0:0:0:0/block/sdb/sdb1", true},
		{"external SD over MMC host 1", "/sys/devices/platform/fe2c0000.mmc/mmc_host/mmc1/mmc1:aaaa/block/mmcblk1/mmcblk1p1", true},
		{"internal eMMC host 0", "/sys/devices/platform/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p12", false},
		{"internal eMMC on standalone path", "/sys/devices/platform/mmc_host/mmc0/block/mmcblk0/mmcblk0p12", false},
		{"unverified MMC host 0 platform", "/sys/devices/platform/fe2c0000.mmc/mmc_host/mmc0/mmc0:aaaa/block/mmcblk0/mmcblk0p1", false},
		{"SATA destination", "/sys/devices/platform/fe210000.sata/ata1/host1/block/sda/sda1", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			volume := mountedBlockVolume{sysfsPath: test.path, usb: hasUSBAncestor(test.path), sdCard: hasSDCardAncestor(test.path)}
			if got := isRemovableMediaSource(volume); got != test.want {
				t.Fatalf("isRemovableMediaSource(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}
