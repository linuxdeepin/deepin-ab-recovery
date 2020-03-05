package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"pkg.deepin.io/lib/strv"
	"runtime"
	"strconv"
	"strings"
	"time"

	"./grubcfg"
	login1 "github.com/linuxdeepin/go-dbus-factory/org.freedesktop.login1"
	"golang.org/x/xerrors"
	"pkg.deepin.io/dde/api/inhibit_hint"
	"pkg.deepin.io/lib/dbus1"
	"pkg.deepin.io/lib/dbusutil"
	"pkg.deepin.io/lib/log"
	"pkg.deepin.io/lib/procfs"
	"pkg.deepin.io/lib/utils"
)

var logger = log.NewLogger("ab-recovery")

const (
	configFile            = "/etc/deepin/ab-recovery.json"
	backupMountPoint      = "/deepin-ab-recovery-backup"
	abRecoveryGrubCfgFile = "/etc/default/grub.d/11_deepin_ab_recovery.cfg"
)

var globalNoGrubMkconfig bool

// 不支持固件为 PMON 的龙芯机器，如果 globalUsePmonBios 为 true，则CanBackup() 和 CanRestore() 都返回 false。
var globalUsePmonBios bool

var globalArch string
var globalGrubCfgFile = "/boot/grub/grub.cfg"
var globalBootDir = "/boot"
var globalKernelBackupDir string
var globalGrubMenuEn bool

var options struct {
	noRsync        bool
	noGrubMkconfig bool
	arch           string
	grubCfgFile    string
	bootDir        string
	grubMenuEn     bool
}

func init() {
	flag.BoolVar(&options.noRsync, "no-rsync", false, "")
	flag.BoolVar(&options.noGrubMkconfig, "no-grub-mkconfig", false, "")
	flag.BoolVar(&options.grubMenuEn, "grub-menu-en", false, "grub menu entry use english")
	flag.StringVar(&options.arch, "arch", "", "")
	flag.StringVar(&options.grubCfgFile, "grub-cfg", "", "")
	flag.StringVar(&options.bootDir, "boot", "", "")
}

func main() {
	flag.Parse()
	err := os.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	if err != nil {
		logger.Warning("failed to set env PATH", err)
	}

	globalArch = runtime.GOARCH
	if options.arch != "" {
		globalArch = options.arch
	}

	if isArchMips() {
		// mips64

		bi, err := readBoardInfo()
		if err != nil {
			logger.Warning("failed to read board info:", err)
		} else {
			if strings.Contains(bi.biosVersion, "PMON") {
				globalUsePmonBios = true
			}
		}
	} else if isArchSw() {
		globalNoGrubMkconfig = true
	} else if isArchArm() {
		globalGrubMenuEn = true
	}

	if options.noGrubMkconfig {
		globalNoGrubMkconfig = true
	}
	if options.grubCfgFile != "" {
		globalGrubCfgFile = options.grubCfgFile
	}
	if options.grubMenuEn {
		globalGrubMenuEn = true
	}

	if options.bootDir != "" {
		globalBootDir = filepath.Clean(options.bootDir)
	}
	globalKernelBackupDir = filepath.Join(globalBootDir, "deepin-ab-recovery")

	logger.Debug("arch:", globalArch)
	logger.Debug("noGrubMkConfig:", globalNoGrubMkconfig)
	logger.Debug("usePmonBios:", globalUsePmonBios)
	logger.Debug("bootDir:", globalBootDir)
	logger.Debug("grubCfgFile:", globalGrubCfgFile)
	logger.Debug("grubMenuEn:", globalGrubMenuEn)

	service, err := dbusutil.NewSystemService()
	if err != nil {
		logger.Fatal("failed to new system service:", err)
	}

	m := newManager(service)
	err = service.Export(dbusPath, m)
	if err != nil {
		logger.Fatal("failed to export manager:", err)
	}

	ihObj := inhibit_hint.New("deepin-ab-recovery")
	ihObj.SetIcon("preferences-system")
	ihObj.SetName(Tr("Control Center"))
	err = ihObj.Export(service)
	if err != nil {
		logger.Warning("failed to export inhibit hint:", err)
	}

	err = service.RequestName(dbusServiceName)
	if err != nil {
		logger.Fatal("failed to request service name:", err)
	}

	service.SetAutoQuitHandler(3*time.Minute, m.canQuit)
	service.Wait()
}

func backup(cfg *Config, envVars []string) error {
	backupUuid := cfg.Backup
	backupDevice, err := getDeviceByUuid(backupUuid)
	if err != nil {
		return xerrors.Errorf("failed to get backup device by uuid %q: %w", backupUuid, err)
	}

	logger.Debug("backup device:", backupDevice)

	mounted, err := isMounted(backupMountPoint)
	if err != nil {
		return err
	}

	if mounted {
		err = exec.Command("umount", backupMountPoint).Run()
		if err != nil {
			return xerrors.Errorf("failed to unmount %s: %w", backupMountPoint, err)
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
		return xerrors.Errorf("failed to mount device %q to dir %q: %w",
			backupDevice, backupMountPoint, err)
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
		return xerrors.Errorf("failed to write exclude file: %w", err)
	}
	defer func() {
		err := os.Remove(tmpExcludeFile)
		if err != nil {
			logger.Warning("failed to remove temporary exclude file:", err)
		}
	}()

	osVersion := "unknown"
	osDesc := "Uos unknown"
	lsbReleaseInfo, err := runLsbRelease()
	if err != nil {
		logger.Warning("failed to run lsb-release:", err)
	} else {
		osVersion = lsbReleaseInfo[lsbReleaseKeyRelease]
		osDesc = lsbReleaseInfo[lsbReleaseKeyDesc]
	}

	now := time.Now()
	cfg.Time = &now
	cfg.Version = osVersion
	err = cfg.save(configFile)
	if err != nil {
		return xerrors.Errorf("failed to save config file %q: %w", configFile, err)
	}

	err = runRsync(tmpExcludeFile)
	if err != nil {
		return xerrors.Errorf("run rsync err: %w", err)
	}

	for _, dir := range skipDirs {
		dir := filepath.Join(backupMountPoint, dir)
		err = os.Mkdir(dir, 0755)
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
		return xerrors.Errorf("failed to modify fs tab: %w", err)
	}

	kFiles, err := backupKernel()
	if err != nil {
		return xerrors.Errorf("failed to backup kernel: %w", err)
	}

	// generate grub config
	err = writeGrubCfgBackup(backupUuid, backupDevice, osDesc, kFiles, now, envVars)
	if err != nil {
		return xerrors.Errorf("failed to write grub cfg: %w", err)
	}

	return nil
}

func runRsync(excludeFile string) error {
	if options.noRsync {
		logger.Debug("skip run rsync")
		return nil
	}

	logger.Debug("run rsync...")
	var rsyncArgs []string
	if logger.GetLogLevel() == log.LevelDebug {
		rsyncArgs = append(rsyncArgs, "-v")
	}
	rsyncArgs = append(rsyncArgs, "-x", "-a", "--delete-after",
		"--exclude-from="+excludeFile,
		"/", backupMountPoint+"/")

	cmd := exec.Command("rsync", rsyncArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func backupKernel() (kFiles *kernelFiles, err error) {
	err = os.RemoveAll(globalKernelBackupDir + ".old")
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	}

	err = os.Rename(globalKernelBackupDir, globalKernelBackupDir+".old")
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	}

	err = os.Mkdir(globalKernelBackupDir, 0755)
	if err != nil {
		return
	}

	// find current kernel
	utsName, err := uname()
	if err != nil {
		return
	}
	release := utsName.release
	bootOpts, err := getBootOptions()
	if err == nil {
		releaseBo := getKernelReleaseWithBootOption(bootOpts)
		if releaseBo != "" {
			release = releaseBo
		}
	} else {
		logger.Warning(err)
	}

	kFiles, err = findKernelFiles(release, utsName.machine)
	if err != nil {
		return
	}

	logger.Debug("found linux:", kFiles.linux)
	logger.Debug("found initrd:", kFiles.initrd)

	// copy linux
	linuxBackup := filepath.Join(globalKernelBackupDir, filepath.Base(kFiles.linux))
	err = utils.CopyFile(kFiles.linux, linuxBackup)
	if err != nil {
		return
	}

	// copy initrd
	initrdBackup := filepath.Join(globalKernelBackupDir, filepath.Base(kFiles.initrd))
	err = utils.CopyFile(kFiles.initrd, initrdBackup)
	if err != nil {
		return
	}

	err = os.RemoveAll(globalKernelBackupDir + ".old")
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

func getKernelReleaseWithBootOption(options string) string {
	var bootImg string
	for _, part := range strings.Split(options, " ") {
		if strings.HasPrefix(part, "BOOT_IMAGE=") {
			bootImg = strings.TrimSpace(strings.TrimPrefix(part, "BOOT_IMAGE="))
			break
		}
	}
	if bootImg == "" {
		return ""
	}
	bootImg = filepath.Base(bootImg)
	var result string
	for _, prefix := range []string{"vmlinuz-", "vmlinux-", "kernel-"} {
		result = strings.TrimPrefix(bootImg, prefix)
		if len(result) != len(bootImg) {
			return result
		}
	}
	return ""
}

func findKernelFilesAux(release, machine string, files strv.Strv) (*kernelFiles, error) {
	var result kernelFiles
	prefixes := []string{"vmlinuz-", "vmlinux-", "kernel-"}
	switch machine {
	case "i386", "i686", "x86_64":
		prefixes = []string{"vmlinuz-", "kernel-"}
	}

	// linux
	for _, prefix := range prefixes {
		fileBasename := prefix + release
		if files.Contains(fileBasename) {
			filename := filepath.Join(globalBootDir, fileBasename)
			result.linux = filename
			break
		}
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
		fileBasename := replacer.Replace(format)
		if files.Contains(fileBasename) {
			filename := filepath.Join(globalBootDir, fileBasename)
			result.initrd = filename
			break
		}
	}
	if result.initrd == "" {
		return nil, errors.New("findKernelFiles: not found initrd")
	}

	return &result, nil
}

func findKernelFiles(release, machine string) (*kernelFiles, error) {
	fileInfoList, err := ioutil.ReadDir(globalBootDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, info := range fileInfoList {
		if info.IsDir() {
			continue
		}
		files = append(files, info.Name())
	}
	return findKernelFilesAux(release, machine, files)
}

func restore(cfg *Config, envVars []string) error {
	currentDevice, err := getDeviceByUuid(cfg.Current)
	if err != nil {
		return xerrors.Errorf("failed to get device by uuid %q: %w", cfg.Current, err)
	}

	err = writeGrubCfgRestore(cfg.Current, currentDevice, cfg.Backup, envVars)
	if err != nil {
		return xerrors.Errorf("failed to write grub cfg: %w", err)
	}

	// swap current and backup
	cfg.Current, cfg.Backup = cfg.Backup, cfg.Current
	cfg.Time = nil
	cfg.Version = ""
	err = cfg.save(configFile)
	if err != nil {
		return xerrors.Errorf("failed to save config file %q: %w", configFile, err)
	}

	return nil
}

func writeGrubCfgRestore(currentUuid, currentDevice, backupUuid string, envVars []string) error {
	if globalNoGrubMkconfig {
		if isArchMips() {
			return writeGrubCfgRestoreMips(backupUuid)
		} else if isArchSw() {
			return writeGrubCfgRestoreSw(backupUuid)
		} else {
			return nil
		}
	}

	filename := abRecoveryGrubCfgFile
	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n",
		currentUuid, currentDevice))

	err = ioutil.WriteFile(filename, buf.Bytes(), 0644)
	if err != nil {
		return err
	}

	err = runUpdateGrub(envVars)
	return err
}

func writeGrubCfgRestoreSw(uuid string) error {
	return writeGrubCfgRestoreMips(uuid)
}

func writeGrubCfgRestoreMips(uuid string) error {
	cfg, err := grubcfg.ParseGrubCfgFile(globalGrubCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to parse grub cfg file: %w", err)
	}

	cfg.RemoveRecoveryMenuEntries()
	err = cfg.ReplaceRootUuid(uuid)
	if err != nil {
		return xerrors.Errorf("failed to replace root uuid: %w", err)
	}

	err = cfg.Save(globalGrubCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to save grub cfg file: %w", err)
	}
	return nil
}

func writeGrubCfgBackup(backupUuid, backupDevice, osDesc string,
	kFiles *kernelFiles, backupTime time.Time, envVars []string) error {
	if globalGrubMenuEn {
		envVars = []string{"LANG=en_US.UTF-8", "LANGUAGE=en_US"}
	}

	if globalNoGrubMkconfig {
		if isArchSw() {
			return writeGrubCfgBackupSw(backupUuid, osDesc, kFiles, backupTime, envVars)
		} else if isArchMips() {
			return writeGrubCfgBackupMips(backupUuid, osDesc, kFiles, backupTime)
		} else {
			return nil
		}
	}

	filename := abRecoveryGrubCfgFile
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
	buf.WriteString(varPrefix + "LINUX=\"" + filepath.Join(globalKernelBackupDir,
		filepath.Base(kFiles.linux)) + "\"\n")
	buf.WriteString(varPrefix + "INITRD=\"" + filepath.Base(kFiles.initrd) + "\"\n")
	buf.WriteString(varPrefix + "OS_DESC=\"" + osDesc + "\"\n")
	buf.WriteString(varPrefix + "BACKUP_TIME=" + strconv.FormatInt(backupTime.Unix(), 10))

	err = ioutil.WriteFile(filename, buf.Bytes(), 0644)
	if err != nil {
		return xerrors.Errorf("failed to write file %q: %w", filename, err)
	}

	err = runUpdateGrub(envVars)
	if err != nil {
		return xerrors.Errorf("run update-grub err: %w", err)
	}

	return nil
}

func writeGrubCfgBackupSw(backupUuid string, osDesc string, kFiles *kernelFiles,
	backupTime time.Time, envVars []string) error {
	cfg, err := grubcfg.ParseGrubCfgFile(globalGrubCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to parse grub cfg file: %w", err)
	}

	cfg.RemoveRecoveryMenuEntries()

	menuText := getRollBackMenuTextSafe(osDesc, backupTime, envVars)
	dir := strings.TrimPrefix(globalKernelBackupDir, globalBootDir+"/")
	linux := filepath.Join(dir, filepath.Base(kFiles.linux))
	initrd := filepath.Join(dir, filepath.Base(kFiles.initrd))
	cfg.AddRecoveryMenuEntrySw(menuText, backupUuid, linux, initrd)

	err = cfg.Save(globalGrubCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to save grub cfg file: %w", err)
	}
	return nil
}

func writeGrubCfgBackupMips(backupUuid string, osDesc string, kFiles *kernelFiles, backupTime time.Time) error {
	cfg, err := grubcfg.ParseGrubCfgFile(globalGrubCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to parse grub cfg file: %w", err)
	}

	cfg.RemoveRecoveryMenuEntries()

	menuText := getRollbackMenuTextForceEn(osDesc, backupTime)
	dir := strings.TrimPrefix(globalKernelBackupDir, globalBootDir+"/")
	linux := filepath.Join(dir, filepath.Base(kFiles.linux))
	initrd := filepath.Join(dir, filepath.Base(kFiles.initrd))
	cfg.AddRecoveryMenuEntryMips(menuText, backupUuid, linux, initrd)

	err = cfg.Save(globalGrubCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to save grub cfg file: %w", err)
	}
	return nil
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

func getRootUuid() (string, error) {
	out, err := exec.Command("grub-probe", "-t", "fs_uuid", "/").Output()
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
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

func getRollBackMenuText(osDesc string, backupTime time.Time, envVars []string) (string, error) {
	cmd := exec.Command("gettext", "-d", "deepin-ab-recovery", msgRollBack)
	cmd.Env = append(cmd.Env, envVars...)
	getTextOut, err := cmd.Output()
	if err != nil {
		return "", xerrors.Errorf("run gettext error: %w", err)
	}
	getTextOut = bytes.TrimSpace(getTextOut)

	backupTs := strconv.FormatInt(backupTime.Unix(), 10)
	cmd = exec.Command("date", "+%c", "-d", "@"+backupTs)
	cmd.Env = append(cmd.Env, envVars...)
	dateOut, err := cmd.Output()
	if err != nil {
		return "", xerrors.Errorf("run date error: %w", err)
	}
	dateOut = bytes.TrimSpace(dateOut)
	return fmt.Sprintf(string(getTextOut), osDesc, dateOut), nil
}

func getRollBackMenuTextSafe(osDesc string, backupTime time.Time, envVars []string) string {
	str, err := getRollBackMenuText(osDesc, backupTime, envVars)
	if err != nil {
		logger.Warning(err)
		return getRollbackMenuTextForceEn(osDesc, backupTime)
	}
	return str
}

func getRollbackMenuTextForceEn(osDesc string, backupTime time.Time) string {
	dateTimeStr := backupTime.Format("Mon 02 Jan 2006 03:04:05 PM MST")
	return fmt.Sprintf(msgRollBack, osDesc, dateTimeStr)
}
