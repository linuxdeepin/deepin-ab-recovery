package main

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUtilLog(t *testing.T) {
	// 重定向日志到临时文件
	testDataPath := "./TemporaryTestDataDirectoryNeedDelete"
	err := os.Mkdir(testDataPath, 0777)
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(testDataPath)
		require.NoError(t, err)
	}()
	tmpfile, err := ioutil.TempFile(testDataPath, "test.log")
	require.NoError(t, err)
	defer tmpfile.Close()

	os.Stdout = tmpfile
	os.Stderr = tmpfile
	defer func() {
		// 恢复标准输出
		os.Stdout = os.NewFile(uintptr(syscall.Stdout), "/dev/stdout")
		os.Stderr = os.NewFile(uintptr(syscall.Stderr), "/dev/stderr")
	}()
	// 测试log
	setLogEnv(logEnvGrubMkconfig)
	testData := "this is a test.abcd"
	logWarningf(testData)
	logData, err := ioutil.ReadFile(tmpfile.Name())
	require.NoError(t, err)
	// logWarningf会补\n
	assert.Equal(t, testData+"\n", string(logData))
}

func TestUtilArch(t *testing.T) {
	arch := globalArch

	globalArch = "sw_64"
	assert.True(t, isArchSw())

	globalArch = "mips10"
	assert.True(t, isArchMips())

	globalArch = "arm10"
	assert.True(t, isArchArm())

	globalArch = arch
}

func TestUtilDiskDevice(t *testing.T) {
	filepathNames, err := filepath.Glob(filepath.Join("/dev/disk/by-uuid", "*"))
	if err != nil || len(filepathNames) == 0 {
		// 没有找到则无法继续测试，不能认为是hasDiskDevice()函数测试失败
		t.Skip("can not find /dev/disk/by-uuid")
	}
	for i := range filepathNames {
		devUUID := path.Base(filepathNames[i])
		isFind := hasDiskDevice(devUUID)
		if !isFind {
			continue
		}
		name, err := getDeviceByUuid(devUUID)
		require.NoError(t, err)
		assert.NotEmpty(t, name)
		_, err = getDeviceLabel(name)
		require.NoError(t, err)
	}
}

func TestUtilOsProber(t *testing.T) {
	devices, err := runOsProber()
	if err != nil {
		t.Skip("need root")
	}
	for _, device := range devices {
		uuid, err := getDeviceUuid(device)
		if err != nil {
			continue
		}
		assert.NotEmpty(t, uuid)
	}
}

func TestUtilRunOsRelease(t *testing.T) {
	ret, err := runOsRelease()
	if err != nil {
		t.Skip("")
	}
	assert.NotEqual(t, len(ret), 0)
}

func TestUtilParseOsReleaseOutput(t *testing.T) {
	testData := map[string]string{"SystemName": "UnionTech OS Desktop", "SystemName[zh_CN]": "统信桌面操作系统", "ProductType": "Desktop", "ProductType[zh_CN]": "桌面", "EditionName": "Professional", "EditionName[zh_CN]": "专业版", "MajorVersion": "20", "MinorVersion": "1040", "OsBuild": "11018.101"}
	dataStr := "[Version]\n"
	for k, v := range testData {
		dataStr = dataStr + k + "=" + v + "\n"
	}
	ret := parseOsReleaseOutput([]byte(dataStr))
	for k, v := range ret {
		assert.Equal(t, testData[k], v)
	}
}

func TestUtilPathDisk(t *testing.T) {
	rootDisk, err := getPathDisk("/")
	if err != nil {
		t.Skip("can not find grub-probe")
	}
	_, err = getLabelUuidMap(rootDisk)
	require.NoError(t, err)
}

func TestUtilMount(t *testing.T) {
	_, err := ioutil.ReadFile("/proc/self/mounts")
	if err != nil {
		t.Skip("can not read /proc/self/mounts")
	}
	_, err = isMounted("/")
	require.NoError(t, err)
	_, err = isMountedRo("/")
	require.NoError(t, err)
}

func TestUtilBoard(t *testing.T) {
	info, err := readBoardInfo()
	if err != nil {
		t.Skip("")
	}
	assert.NotEmpty(t, info)
}

func TestUtilParseBoardInfo(t *testing.T) {
	testData := map[string]string{"Version": "1.2.3", "Info": "test"}
	dataStr := ""
	for k, v := range testData {
		dataStr = dataStr + k + ":" + v + "\n"
	}
	info := parseBoardInfo([]byte(dataStr))
	assert.Equal(t, info.biosVersion, "1.2.3")
}

func TestUtilBootOptions(t *testing.T) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		t.Skip("can not read /proc/cmdline")
	}
	content2, err := getBootOptions()
	require.NoError(t, err)
	assert.Equal(t, string(content), content2)
}
