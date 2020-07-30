package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/xerrors"
	"pkg.deepin.io/lib/strv"
)

const (
	logEnvCommon = iota
	logEnvGrubMkconfig
)

var _logEnv = logEnvCommon

func logWarningf(format string, args ...interface{}) {
	if _logEnv == logEnvGrubMkconfig {
		_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	} else {
		logger.Warningf(format, args...)
	}
}

func setLogEnv(logEnv int) {
	_logEnv = logEnv
}

func isArchSw() bool {
	return globalArch == "sw_64"
}

func isArchMips() bool {
	return strings.HasPrefix(globalArch, "mips")
}

func isArchArm() bool {
	return strings.HasPrefix(globalArch, "arm")
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
	if globalNoGrubMkconfig {
		return nil
	}

	var cmd *exec.Cmd
	updateGrubBin, err := exec.LookPath("update-grub")
	if err == nil {
		// found update-grub
		logger.Debug("$ ", updateGrubBin)
		cmd = exec.Command(updateGrubBin)
	} else {
		// not found update-grub
		logger.Warning("not found command update-grub")
		logger.Debug("$ grub-mkconfig -o /boot/grub/grub.cfg")
		cmd = exec.Command("grub-mkconfig", "-o", "/boot/grub/grub.cfg")
	}

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

func getDeviceUuid(device string) (string, error) {
	out, err := exec.Command("grub-probe", "-t", "fs_uuid", "-d", device).Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
}

func getDeviceByUuid(uuid string) (string, error) {
	if uuid == "" {
		return "", xerrors.New("parameter uuid is empty")
	}
	out, err := exec.Command("lsblk", "-P", "-n", "-o", "UUID,PATH").Output()
	if err != nil {
		return "", xerrors.Errorf("failed to run lsblk: %w", err)
	}
	devPath := getPathFromLsblkOutput(string(out), uuid)
	if devPath == "" {
		return "", xerrors.New("failed to get device path from lsblk output")
	}
	return devPath, nil
}

func getPathFromLsblkOutput(out string, uuid string) string {
	if uuid == "" {
		return ""
	}
	lines := strings.Split(out, "\n")
	uuidSubstr := fmt.Sprintf("UUID=%q", uuid)
	for _, line := range lines {
		if strings.Contains(line, uuidSubstr) {
			pathReg := regexp.MustCompile(`PATH="(.+)"`)
			match := pathReg.FindStringSubmatch(line)
			if match != nil {
				return match[1]
			}
			break
		}
	}
	return ""
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

func isMountedRo(mountPoint string) (bool, error) {
	content, err := ioutil.ReadFile("/proc/self/mounts")
	if err != nil {
		return false, err
	}
	return isMountedRoAux(content, mountPoint), nil
}

func isMountedRoAux(data []byte, mountPoint string) bool {
	lines := bytes.Split(data, []byte{'\n'})
	for _, line := range lines {
		fields := bytes.Split(line, []byte{' '})
		if len(fields) >= 4 {
			if string(fields[1]) == mountPoint {
				optionsStr := string(fields[3])
				options := strv.Strv(strings.Split(optionsStr, ","))
				if options.Contains("ro") {
					return true
				}
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

const (
	osSystemNameZHCN  = "SystemName[zh_CN]"
	osProductType     = "ProductType"
	osEditionName     = "EditionName"
	osMinorVersion    = "MinorVersion"
	osOsBuild         = "OsBuild"
	osSystemName      = "SystemName"
	osProductTypeZHCH = "ProductType"
	osEditionNameZHCH = "EditionName"
	osMajorVersion    = "MajorVersion"
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

func getBootOptions() (string, error) {
	content, err := ioutil.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func parseOsProberOutput(data []byte) []string {
	lines := bytes.Split(data, []byte{'\n'})
	var devices []string
	for _, line := range lines {
		fields := strings.SplitN(string(line), ":", 4)
		if len(fields) < 4 {
			continue
		}
		device := fields[0]
		label := strings.ToLower(fields[2])
		boot := strings.ToLower(fields[3])
		if (label == "uos" || label == "deepin") && boot == "linux" {
			devices = append(devices, device)
		}
	}
	return devices
}

func runOsProber() ([]string, error) {
	out, err := exec.Command("os-prober").Output()
	if err != nil {
		return nil, err
	}
	result := parseOsProberOutput(out)
	return result, nil
}
func runOsRelease() (map[string]string, error) {
	content, err := ioutil.ReadFile("/etc/os-version")
	if err != nil {
		return nil, err
	}
	result := parseOsReleaseOutput(content)
	return result, nil
}

func parseOsReleaseOutput(data []byte) map[string]string {
	lines := strings.Split(string(data), "\n")
	result := make(map[string]string)
	for _, line := range lines {
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]
		result[key] = value
	}
	return result
}
