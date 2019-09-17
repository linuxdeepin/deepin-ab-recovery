package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	login1 "github.com/linuxdeepin/go-dbus-factory/org.freedesktop.login1"
	"pkg.deepin.io/dde/api/inhibit_hint"
	"pkg.deepin.io/lib/dbus1"
	"pkg.deepin.io/lib/dbusutil"
	"pkg.deepin.io/lib/keyfile"
	"pkg.deepin.io/lib/log"
	"pkg.deepin.io/lib/procfs"
	"pkg.deepin.io/lib/utils"
)

var logger = log.NewLogger("ab-recovery")

const (
	configFile       = "/etc/deepin/ab-recovery.json"
	backupMountPoint = "/deepin-ab-recovery-backup"
	grubCfgFile      = "/etc/default/grub.d/11_deepin_ab_recovery.cfg"
	kernelBackupDir  = "/boot/deepin-ab-recovery"
)

func main() {
	err := os.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	if err != nil {
		logger.Warning(err)
	}

	service, err := dbusutil.NewSystemService()
	if err != nil {
		logger.Fatal(err)
	}

	m := newManager(service)
	err = service.Export(dbusPath, m)
	if err != nil {
		logger.Warning(err)
	}

	ihObj := inhibit_hint.New("deepin-ab-recovery")
	ihObj.SetIcon("dde-control-center")
	ihObj.SetName(Tr("Control Center"))
	err = ihObj.Export(service)
	if err != nil {
		logger.Warning(err)
	}

	err = service.RequestName(dbusServiceName)
	if err != nil {
		logger.Fatal(err)
	}

	service.SetAutoQuitHandler(3*time.Minute, m.canQuit)
	service.Wait()
}

func isMounted(mountPoint string) (bool, error) {
	content, err := ioutil.ReadFile("/proc/self/mounts")
	if err != nil {
		return false, err
	}
	lines := bytes.Split(content, []byte{'\n'})
	for _, line := range lines {
		fields := bytes.SplitN(line, []byte{' '}, 3)
		if len(fields) >= 2 {
			if string(fields[1]) == mountPoint {
				return true, nil
			}
		}
	}
	return false, nil
}

func backup(cfg *Config, envVars []string) error {
	backupUuid := cfg.Backup
	backupDevice, err := getDeviceByUuid(backupUuid)
	if err != nil {
		return err
	}

	logger.Debug("backup device:", backupDevice)

	mounted, err := isMounted(backupMountPoint)
	if err != nil {
		return err
	}

	if mounted {
		err = exec.Command("umount", backupMountPoint).Run()
		if err != nil {
			return err
		}
	}

	err = os.Mkdir(backupMountPoint, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	defer func() {
		err = os.Remove(backupMountPoint)
		if err != nil {
			logger.Warning("failed to remove backup mount point:", err)
		}
	}()

	err = exec.Command("mount", backupDevice, backupMountPoint).Run()
	if err != nil {
		return err
	}
	defer func() {
		err := exec.Command("umount", backupMountPoint).Run()
		if err != nil {
			logger.Warning("failed to unmount backup directory:", err)
		}
	}()

	skipDirs := []string{
		"/media", "/tmp",
	}

	tmpExcludeFile, err := writeExcludeFile(append(skipDirs, backupMountPoint))
	if err != nil {
		return err
	}
	defer func() {
		err := os.Remove(tmpExcludeFile)
		if err != nil {
			logger.Warning("failed to remove temporary exclude file:", err)
		}
	}()

	deepinVersion, err := getDeepinVersion("/etc/deepin-version")
	if err != nil {
		logger.Warning(err)
		deepinVersion = "unknown"
	}

	now := time.Now()
	cfg.Time = &now
	cfg.Version = deepinVersion
	err = cfg.save(configFile)
	if err != nil {
		return err
	}

	logger.Debug("run rsync...")
	var rsyncArgs []string
	if logger.GetLogLevel() == log.LevelDebug {
		rsyncArgs = append(rsyncArgs, "-v")
	}
	rsyncArgs = append(rsyncArgs, "-x", "-a", "--delete-after",
		"--exclude-from="+tmpExcludeFile,
		"/", backupMountPoint+"/")

	cmd := exec.Command("rsync", rsyncArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	for _, dir := range skipDirs {
		err = os.Mkdir(filepath.Join(backupMountPoint, dir), 0755)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
	}

	// modify fs tab
	err = modifyFsTab(filepath.Join(backupMountPoint, "etc/fstab"), backupUuid, backupDevice)
	if err != nil {
		return err
	}

	kFiles, err := backupKernel()
	if err != nil {
		return err
	}

	// generate grub config
	err = writeGrubCfgBackup(grubCfgFile, backupUuid, backupDevice, deepinVersion, kFiles, now)
	if err != nil {
		return err
	}

	err = runUpdateGrub(envVars)
	if err != nil {
		return err
	}

	return nil
}

func getDeepinVersion(filename string) (string, error) {
	kf := keyfile.NewKeyFile()
	err := kf.LoadFromFile(filename)
	if err != nil {
		return "", err
	}
	return kf.GetString("Release", "Version")
}

func backupKernel() (kFiles *kernelFiles, err error) {
	err = os.RemoveAll(kernelBackupDir + ".old")
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	}

	err = os.Rename(kernelBackupDir, kernelBackupDir+".old")
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	}

	err = os.Mkdir(kernelBackupDir, 0755)
	if err != nil {
		return
	}

	// find current kernel
	utsName, err := uname()
	if err != nil {
		return
	}
	kFiles, err = findKernelFiles(utsName.release,
		utsName.machine)
	if err != nil {
		return
	}

	logger.Debug("found linux:", kFiles.linux)
	logger.Debug("found initrd:", kFiles.initrd)

	// copy linux
	linuxBackup := filepath.Join(kernelBackupDir, filepath.Base(kFiles.linux))
	err = utils.CopyFile(kFiles.linux, linuxBackup)
	if err != nil {
		return
	}

	// copy initrd
	initrdBackup := filepath.Join(kernelBackupDir, filepath.Base(kFiles.initrd))
	err = utils.CopyFile(kFiles.initrd, initrdBackup)
	if err != nil {
		return
	}

	err = os.RemoveAll(kernelBackupDir + ".old")
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	}

	return
}

type kernelFiles struct {
	linux  string
	initrd string
}

func getGenKernelArch(machine string) string {
	switch machine {
	case "i386", "i686":
		return "x86"
	case "mips", "mips64":
		return "mips"
	case "mipsel", "mips64el":
		return "mips"
	default:
		if strings.HasPrefix(machine, "arm") {
			return "arm"
		}
		return machine
	}
}

func findKernelFiles(release, machine string) (*kernelFiles, error) {
	var result kernelFiles
	prefixes := []string{"vmlinuz-", "vmlinux-", "kernel-"}
	switch machine {
	case "i386", "i686", "x86_64":
		prefixes = []string{"vmlinuz-", "kernel-"}
	}

	// linux
	for _, prefix := range prefixes {
		filename := filepath.Join("/boot", prefix+release)
		_, err := os.Stat(filename)
		if err != nil {
			continue
		}

		result.linux = filename
		break
	}

	if result.linux == "" {
		return nil, errors.New("findKernelFiles: not found linux")
	}

	// initrd
	altVersion := strings.TrimSuffix(release, ".old")
	genKernelArch := getGenKernelArch(machine)
	replacer := strings.NewReplacer("${version}", release,
		"${altVersion}", altVersion,
		"${genKernelArch}", genKernelArch)
	for _, format := range []string{
		"initrd.img-${version}", "initrd-${version}.img", "initrd-${version}.gz",
		"initrd-${version}", "initramfs-${version}.img",
		"initrd.img-${altVersion}", "initrd-${altVersion}.img",
		"initrd-${altVersion}", "initramfs-${altVersion}.img",
		"initramfs-genkernel-${version}",
		"initramfs-genkernel-${altVersion}",
		"initramfs-genkernel-${genKernelArch}-${version}",
		"initramfs-genkernel-${genKernelArch}-${altVersion}",
	} {
		filename := filepath.Join("/boot", replacer.Replace(format))
		_, err := os.Stat(filename)
		if err != nil {
			continue
		}

		result.initrd = filename
		break
	}
	if result.initrd == "" {
		return nil, errors.New("findKernelFiles: not found initrd")
	}

	return &result, nil
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
	cmd := exec.Command("grub-mkconfig", "-o", "/boot/grub/grub.cfg")
	cmd.Env = append(os.Environ(), envVars...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func restore(cfg *Config, envVars []string) error {
	currentDevice, err := getDeviceByUuid(cfg.Current)
	if err != nil {
		return err
	}

	err = writeGrubCfgRestore(grubCfgFile, cfg.Current, currentDevice)
	if err != nil {
		return err
	}

	err = runUpdateGrub(envVars)
	if err != nil {
		return err
	}

	// swap current and backup
	cfg.Current, cfg.Backup = cfg.Backup, cfg.Current
	cfg.Time = nil
	cfg.Version = ""
	err = cfg.save(configFile)
	if err != nil {
		return err
	}

	return nil
}

func writeGrubCfgRestore(filename, uuid, device string) error {
	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n",
		uuid, device))

	err = ioutil.WriteFile(filename, buf.Bytes(), 0644)
	return err
}

func writeGrubCfgBackup(filename, backupUuid, backupDevice, deepinVersion string,
	kFiles *kernelFiles, backupTime time.Time) error {
	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	const varPrefix = "DEEPIN_AB_RECOVERY_"
	var buf bytes.Buffer
	buf.WriteString(varPrefix + "BACKUP_UUID=" + backupUuid + "\n")
	buf.WriteString(fmt.Sprintf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n",
		backupUuid, backupDevice))
	buf.WriteString(varPrefix + "LINUX=\"" + filepath.Join(kernelBackupDir,
		filepath.Base(kFiles.linux)) + "\"\n")
	buf.WriteString(varPrefix + "INITRD=\"" + filepath.Base(kFiles.initrd) + "\"\n")
	buf.WriteString(varPrefix + "VERSION=\"" + deepinVersion + "\"\n")
	buf.WriteString(varPrefix + "BACKUP_TIME=" + strconv.FormatInt(backupTime.Unix(), 10))

	err = ioutil.WriteFile(filename, buf.Bytes(), 0644)
	return err
}

func modifyFsTab(filename, uuid, device string) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	lines := bytes.Split(content, []byte("\n"))
	modifyDone := false
	for idx, line := range lines {
		l := bytes.TrimSpace(line)
		if bytes.HasPrefix(l, []byte("#")) {
			continue
		}

		fields := bytes.Fields(l)
		if len(fields) >= 2 {
			if bytes.Equal(fields[1], []byte("/")) &&
				bytes.HasPrefix(fields[0], []byte("UUID")) {

				lines[idx] = bytes.Replace(line, fields[0], []byte("UUID="+uuid), 1)

				// set comment line
				if idx-1 >= 0 &&
					bytes.HasPrefix(bytes.TrimSpace(lines[idx-1]), []byte("#")) {
					lines[idx-1] = []byte("# " + device)
				}

				modifyDone = true
				break
			}
		}
	}
	if !modifyDone {
		// 没有找到描述了挂载 / 的行
		return errors.New("not found target line")
	}
	content = bytes.Join(lines, []byte("\n"))
	err = ioutil.WriteFile(filename, content, 0644)
	return err
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

func getRootUuid() (string, error) {
	out, err := exec.Command("grub-probe", "-t", "fs_uuid", "/").Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
}

func getDeviceByUuid(uuid string) (string, error) {
	device, err := filepath.EvalSymlinks(filepath.Join("/dev/disk/by-uuid", uuid))
	return device, err
}

func hasDiskDevice(uuid string) bool {
	if uuid == "" {
		return false
	}
	_, err := os.Stat(filepath.Join("/dev/disk/by-uuid", uuid))
	return err == nil
}

func inhibit(what, who, why string) (dbus.UnixFD, error) {
	systemConn, err := dbus.SystemBus()
	if err != nil {
		return 0, err
	}
	m := login1.NewManager(systemConn)
	return m.Inhibit(0, what, who, why, "block")
}

func getLocaleEnvVarsWithSender(service *dbusutil.Service, sender dbus.Sender) ([]string, error) {
	var result []string

	pid, err := service.GetConnPID(string(sender))
	if err != nil {
		return nil, err
	}

	p := procfs.Process(pid)
	environ, err := p.Environ()
	if err != nil {
		return nil, err
	} else {
		v, ok := environ.Lookup("LANG")
		if ok {
			result = append(result, "LANG="+v)
		}
		v, ok = environ.Lookup("LANGUAGE")
		if ok {
			result = append(result, "LANGUAGE="+v)
		}
	}
	return result, nil
}
