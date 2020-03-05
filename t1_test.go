package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

const boardInfo1 = `BIOS Information
Vendor			: Kunlun
Version			: Kunlun-A1801-V3.1.7-20190716
BIOS ROMSIZE		: 1024
Release date		: 20190716

Base Board Information		
Manufacturer		: LEMOTE
Board name		: LEMOTE-LS3A3000-7A1000-1w-V0.1-pc
Family			: LOONGSON3

`

func TestParseBoardInfo(t *testing.T) {
	info := parseBoardInfo([]byte(boardInfo1))
	assert.Equal(t, "Kunlun-A1801-V3.1.7-20190716", info.biosVersion)
}

const lsbReleaseOutput = `Distributor ID: Deepin
Description:    Deepin 15.10.1
Release:        15.10.1
Codename:       stable
`

func TestParseLsbReleaseOutput(t *testing.T) {
	info := parseLsbReleaseOutput([]byte(lsbReleaseOutput))
	assert.Equal(t, "Deepin", info[lsbReleaseKeyDistID])
	assert.Equal(t, "Deepin 15.10.1", info[lsbReleaseKeyDesc])
	assert.Equal(t, "15.10.1", info[lsbReleaseKeyRelease])
	assert.Equal(t, "stable", info[lsbReleaseKeyCodename])
}

const mountsInfo = `mqueue /dev/mqueue mqueue rw,relatime 0 0
configfs /sys/kernel/config configfs rw,relatime 0 0
hugetlbfs /dev/hugepages hugetlbfs rw,relatime,pagesize=2M 0 0
debugfs /sys/kernel/debug debugfs rw,relatime 0 0
fusectl /sys/fs/fuse/connections fusectl rw,relatime 0 0
binfmt_misc /proc/sys/fs/binfmt_misc binfmt_misc rw,relatime 0 0
/dev/loop0 /snap/core/5145 squashfs ro,nodev,relatime 0 0
/dev/sda2 /home ext4 rw,relatime,data=ordered 0 0
/dev/sda3 /home/tp1/ext ext4 rw,relatime,data=ordered 0 0
tmpfs /run/user/1000 tmpfs rw,nosuid,nodev,relatime,size=790424k,mode=700,uid=1000,gid=1000 0 0
gvfsd-fuse /run/user/1000/gvfs fuse.gvfsd-fuse rw,nosuid,nodev,relatime,user_id=1000,group_id=1000 0 0
/dev/sda5 /media/tp1/19e980bd-a723-4051-bbd9-361a57967657 ext4 rw,nosuid,nodev,relatime,data=ordered 0 0
/dev/fuse /run/user/1000/doc fuse rw,nosuid,nodev,relatime,user_id=1000,group_id=1000 0 0
nvim.appimage /tmp/.mount_vimJNL8U9 fuse.nvim.appimage ro,nosuid,nodev,relatime,user_id=1000,group_id=1000 0 0
`

func TestIsMounted(t *testing.T) {
	assert.True(t, isMountedAux([]byte(mountsInfo), "/home"))
	assert.False(t, isMountedAux([]byte(mountsInfo), "/home/tp1"))
	assert.False(t, isMountedAux([]byte(mountsInfo), "/dev/sda3"))
}

func TestGetDeviceByUuid(t *testing.T) {
	_, err := exec.LookPath("ls")
	if err != nil {
		t.Skip("not found command ls")
	}
	_, err = exec.LookPath("head")
	if err != nil {
		t.Skip("not found command head")
	}

	_, err = os.Stat("/dev/disk")
	if err != nil {
		t.Skip("disk not found")
	}

	out, err := exec.Command("sh", "-c", "ls /dev/disk/by-uuid|head -n1").Output()
	if err != nil {
		t.Error(err)
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		t.Skip("disk not found")
	}
	dev, err := getDeviceByUuid(string(out))
	assert.Nil(t, err)
	t.Log("device:", dev)
}

func TestUname(t *testing.T) {
	utsName, err := uname()
	assert.Nil(t, err)
	t.Log("machine:", utsName.machine)
	t.Log("release:", utsName.release)

	_, err = exec.LookPath("uname")
	if err != nil {
		t.Skip("not found command uname")
	}

	out, err := exec.Command("uname", "-m").Output()
	assert.Nil(t, err)
	out = bytes.TrimSpace(out)
	assert.Equal(t, string(out), utsName.machine)

	out, err = exec.Command("uname", "-r").Output()
	assert.Nil(t, err)
	out = bytes.TrimSpace(out)
	assert.Equal(t, string(out), utsName.release)
}

func TestCharsToString(t *testing.T) {
	assert.Equal(t, "abc", charsToString([]int8{'a', 'b', 'c'}))
	assert.Equal(t, "", charsToString(nil))
	assert.Equal(t, "abc", charsToString([]int8{'a', 'b', 'c', 0, 'd', 'e', 'f'}))
}

func TestWriteExcludeFile(t *testing.T) {
	filename, err := writeExcludeFile([]string{"/boot", "/home"})
	assert.Nil(t, err)
	t.Log("temp filename:", filename)
	content, err := ioutil.ReadFile(filename)
	assert.Nil(t, err)
	assert.Equal(t, []byte(`/boot
/home
`), content)

	err = os.Remove(filename)
	assert.Nil(t, err)
}

func TestFindKernelFiles(t *testing.T) {
	globalBootDir = "/boot"
	result, err := findKernelFilesAux("4.19.0-6-amd64", "x86_64", []string{
		"config-4.19.0-6-amd64", "initrd.img-4.19.0-6-amd64",
		"System.map-4.19.0-6-amd64", "vmlinuz-4.19.0-6-amd64",
	})
	assert.Nil(t, err)
	assert.Equal(t, "/boot/vmlinuz-4.19.0-6-amd64", result.linux)
	assert.Equal(t, "/boot/initrd.img-4.19.0-6-amd64", result.initrd)

	result, err = findKernelFilesAux("4.19.0-arm64-desktop", "aarch64", []string{
		"config-4.19.0-arm64-desktop", "initrd.img-4.19.0-arm64-desktop",
		"initrd.img-4.19.34-1deepin-generic", "dtbo.img",
		"System.map-4.19.0-arm64-desktop", "vmlinuz-4.19.0-arm64-desktop",
	})
	assert.Nil(t, err)
	assert.Equal(t, "/boot/vmlinuz-4.19.0-arm64-desktop", result.linux)
	assert.Equal(t, "/boot/initrd.img-4.19.0-arm64-desktop", result.initrd)
}

func TestGetKernelReleaseWithBootOption(t *testing.T) {
	result := getKernelReleaseWithBootOption("BOOT_IMAGE=/boot/vmlinuz-4.19.0-6-amd64 root=UUID=f18109bb-57ab-4b0f-8bae-a000e59e720a ro splash quiet DEEPIN_GFXMODE=0,1920x1080,1152x864,1600x1200,1280x1024,1024x768")
	assert.Equal(t, "4.19.0-6-amd64", result)

	result = getKernelReleaseWithBootOption("root=UUID=f18109bb-57ab-4b0f-8bae-a000e59e720a ro BOOT_IMAGE=/boot/vmlinuz-4.19.0-6-amd64 splash quiet DEEPIN_GFXMODE=0,1920x1080,1152x864,1600x1200,1280x1024,1024x768")
	assert.Equal(t, "4.19.0-6-amd64", result)

	result = getKernelReleaseWithBootOption("BOOT_IMAGE=/vmlinuz-4.19.0-arm64-desktop root=UUID=f436eb5f-f471-42d9-b750-49987284e4f5 ro splash earlycon=pl011,0xFFF02000 maxcpus=8 initcall_debug=y printktimer=0xfa89b000,0x534,0x538 rcupdate.rcu_expedited=1 buildvariant=eng pmu_nv_addr=0x0 boardid=0x2456 normal_reset_type=fastbootd boot_slice=0x107573 reboot_reason=COLD_BOOT exception_subtype=no last_bootup_keypoint=38 swiotlb=2 dma_zone_only=true kce_status=0 efuse_status=2 nokaslr hhee_enable=false console=ttyAMA6,115200 console=tty quiet loglevel=0 systemd.debug-shell=1 DEEPIN_GFXMODE=")
	assert.Equal(t, "4.19.0-arm64-desktop", result)
}
