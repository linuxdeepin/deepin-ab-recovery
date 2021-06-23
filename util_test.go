package main

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"syscall"
	"testing"

	C "gopkg.in/check.v1"
)

type utilSuite struct{}

func TestDeepinABRecovery(t *testing.T) { C.TestingT(t) }
func init() {
	C.Suite(&utilSuite{})
}

func (*utilSuite) TestSuiteLog(c *C.C) {
	// 重定向日志到临时文件
	dir := c.MkDir()
	tmpfile, err := ioutil.TempFile(dir, "test.log")
	c.Assert(err, C.Equals, nil)
	os.Stdout = tmpfile
	os.Stderr = tmpfile

	// 测试log
	setLogEnv(logEnvGrubMkconfig)
	testData := "this is a test.abcd"
	logWarningf(testData)
	logData, err := ioutil.ReadFile(tmpfile.Name())
	c.Assert(err, C.Equals, nil)
	// logWarningf会补\n
	c.Check(testData+"\n", C.Equals, string(logData))

	// 恢复标准输出
	os.Stdout = os.NewFile(uintptr(syscall.Stdout), "/dev/stdout")
	os.Stderr = os.NewFile(uintptr(syscall.Stderr), "/dev/stderr")
}

func (*utilSuite) TestSuiteUtilArch(c *C.C) {
	arch := globalArch

	globalArch = "sw_64"
	c.Check(isArchSw(), C.Equals, true)

	globalArch = "mips10"
	c.Check(isArchMips(), C.Equals, true)

	globalArch = "arm10"
	c.Check(isArchArm(), C.Equals, true)

	globalArch = arch
}

func (*utilSuite) TestSuiteUtilDiskDevice(c *C.C) {
	filepathNames, err := filepath.Glob(filepath.Join("/dev/disk/by-uuid", "*"))
	if err != nil || len(filepathNames) == 0 {
		// 没有找到则无法继续测试，不能认为是hasDiskDevice()函数测试失败
		c.Skip("can not find /dev/disk/by-uuid")
	}
	for i := range filepathNames {
		devUUID := path.Base(filepathNames[i])
		isFind := hasDiskDevice(devUUID)
		if !isFind {
			continue
		}
		name, err := getDeviceByUuid(devUUID)
		c.Assert(err, C.Equals, nil)
		c.Check(name, C.Not(C.Equals), "")
		_, err = getDeviceLabel(name)
		c.Assert(err, C.Equals, nil)
	}
}

func (*utilSuite) TestSuiteUtilOsProber(c *C.C) {
	devices, err := runOsProber()
	if err != nil {
		c.Skip("need root")
	}
	for _, device := range devices {
		uuid, err := getDeviceUuid(device)
		if err != nil {
			continue
		}
		c.Check(uuid, C.Not(C.Equals), "")
	}
}

func (*utilSuite) TestSuiteUtilRunOsRelease(c *C.C) {
	testData := map[string]string{"SystemName": "UnionTech OS Desktop", "SystemName[zh_CN]": "统信桌面操作系统", "ProductType": "Desktop", "ProductType[zh_CN]": "桌面", "EditionName": "Professional", "EditionName[zh_CN]": "专业版", "MajorVersion": "20", "MinorVersion": "1040", "OsBuild": "11018.101"}
	isFakeData := false
	ret, err := runOsRelease()
	if err != nil {
		if os.IsNotExist(err) {
			dataStr := "[Version]\n"
			for k, v := range testData {
				dataStr = dataStr + k + "=" + v + "\n"
			}
			ret = parseOsReleaseOutput([]byte(dataStr))
			isFakeData = true
		} else {
			c.Assert(err, C.Equals, nil)
		}
	}
	if isFakeData {
		for k, v := range ret {
			c.Check(testData[k], C.Equals, v)
		}
	} else {
		c.Check(len(ret), C.Not(C.Equals), 0)
	}
}

func (*utilSuite) TestSuiteUtilPathDisk(c *C.C) {
	rootDisk, err := getPathDisk("/")
	if err != nil {
		c.Skip("can not find grub-probe")
	}

	_, err = getLabelUuidMap(rootDisk)
	c.Assert(err, C.Equals, nil)
}

func (*utilSuite) TestSuiteUtilMount(c *C.C) {
	_, err := ioutil.ReadFile("/proc/self/mounts")
	if err != nil {
		c.Skip("can not read /proc/self/mounts")
	}
	_, err = isMounted("/")
	c.Assert(err, C.Equals, nil)
	_, err = isMountedRo("/")
	c.Assert(err, C.Equals, nil)
}

func (*utilSuite) TestSuiteUtilBoard(c *C.C) {
	testData := map[string]string{"Version": "1.2.3", "Info": "test"}
	isFakeData := false
	info, err := readBoardInfo()
	if err != nil {
		if os.IsNotExist(err) {
			dataStr := ""
			for k, v := range testData {
				dataStr = dataStr + k + ":" + v + "\n"
			}
			info = parseBoardInfo([]byte(dataStr))
			isFakeData = true
		} else {
			c.Assert(err, C.Equals, nil)
		}
	}
	if isFakeData {
		c.Check(info.biosVersion, C.Equals, "1.2.3")
	}
}

func (*utilSuite) TestSuiteUtilBootOptions(c *C.C) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		c.Skip("can not read /proc/cmdline")
	}
	content2, err := getBootOptions()
	c.Assert(err, C.Equals, nil)
	c.Check(string(content), C.Equals, content2)
}
