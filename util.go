package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func isArchSw() bool {
	return arch == "sw_64"
}

func isArchMips() bool {
	return strings.HasPrefix(arch, "mips")
}

type utsName struct {
	machine string
	release string
}

func uname() (*utsName, error) {
	var buf syscall.Utsname
	err := syscall.Uname(&buf)
	if err != nil {
		return nil, err
	}
	var result utsName
	result.release = charsToString(buf.Release[:])
	result.machine = charsToString(buf.Machine[:])
	return &result, nil
}

func charsToString(ca []int8) string {
	s := make([]byte, 0, len(ca))
	for _, c := range ca {
		if byte(c) == 0 {
			break
		}
		s = append(s, byte(c))
	}
	return string(s)
}

func runUpdateGrub(envVars []string) error {
	if noGrubMkconfig {
		return nil
	}

	cmd := exec.Command("grub-mkconfig", "-o", "/boot/grub/grub.cfg")
	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func writeExcludeFile(excludeItems []string) (string, error) {
	fh, err := ioutil.TempFile("", "deepin-recovery-")
	if err != nil {
		return "", err
	}
	defer fh.Close()

	var buf bytes.Buffer
	for _, item := range excludeItems {
		buf.WriteString(item)
		buf.WriteByte('\n')
	}

	_, err = buf.WriteTo(fh)
	if err != nil {
		return "", err
	}
	return fh.Name(), nil
}

func hasDiskDevice(uuid string) bool {
	if uuid == "" {
		return false
	}
	_, err := os.Stat(filepath.Join("/dev/disk/by-uuid", uuid))
	return err == nil
}

func getDeviceByUuid(uuid string) (string, error) {
	device, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", uuid))
	return device, err
}

func isMounted(mountPoint string) (bool, error) {
	content, err := ioutil.ReadFile("/proc/self/mounts")
	if err != nil {
		return false, err
	}
	return isMountedAux(content, mountPoint), nil
}

func isMountedAux(data []byte, mountPoint string) bool {
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		fields := bytes.SplitN(line, []byte{' '}, 3)
		if len(fields) >= 2 {
			if string(fields[1]) == mountPoint {
				return true
			}
		}
	}
	return false
}

const (
	lsbReleaseKeyDistID   = "Distributor ID"
	lsbReleaseKeyDesc     = "Description"
	lsbReleaseKeyRelease  = "Release"
	lsbReleaseKeyCodename = "Codename"
)

func runLsbRelease() (map[string]string, error) {
	out, err := exec.Command("lsb_release", "-a").Output()
	if err != nil {
		return nil, err
	}
	result := parseLsbReleaseOutput(out)
	return result, nil
}

func parseLsbReleaseOutput(data []byte) map[string]string {
	lines := bytes.Split(data, []byte("\n"))
	result := make(map[string]string)
	for _, line := range lines {
		parts := bytes.SplitN(line, []byte(":"), 2)
		if len(parts) != 2 {
			continue
		}
		key := string(bytes.TrimSpace(parts[0]))
		value := string(bytes.TrimSpace(parts[1]))
		result[key] = value
	}
	return result
}

func readBoardInfo() (*mipsBoardInfo, error) {
	content, err := ioutil.ReadFile("/proc/boardinfo")
	if err != nil {
		return nil, err
	}
	return parseBoardInfo(content), nil
}

type mipsBoardInfo struct {
	biosVersion string
}

func parseBoardInfo(data []byte) *mipsBoardInfo {
	lines := bytes.Split(data, []byte("\n"))
	dict := make(map[string]string)
	for _, line := range lines {
		parts := bytes.SplitN(line, []byte(":"), 2)
		if len(parts) != 2 {
			continue
		}
		key := string(bytes.TrimSpace(parts[0]))
		value := string(bytes.TrimSpace(parts[1]))
		dict[key] = value
	}
	return &mipsBoardInfo{
		biosVersion: dict["Version"],
	}
}
