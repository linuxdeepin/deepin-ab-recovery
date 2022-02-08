/*
 *  Copyright (C) 2019 ~ 2021 Uniontech Software Technology Co.,Ltd
 *
 * Author:
 *
 * Maintainer:
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"./bootloader/grubcfg"
	"./bootloader/pmoncfg"
	"github.com/godbus/dbus"
	"github.com/linuxdeepin/dde-api/inhibit_hint"
	login1 "github.com/linuxdeepin/go-dbus-factory/org.freedesktop.login1"
	"github.com/linuxdeepin/go-lib/dbusutil"
	"github.com/linuxdeepin/go-lib/log"
	"github.com/linuxdeepin/go-lib/procfs"
	"github.com/linuxdeepin/go-lib/strv"
	"github.com/linuxdeepin/go-lib/utils"
	"golang.org/x/xerrors"
)

var logger = log.NewLogger("ab-recovery")

const (
	configFile              = "/etc/deepin/ab-recovery.json"
	backupMountPoint        = "/deepin-ab-recovery-backup"
	abRecoveryGrubCfgFile   = "/etc/default/grub.d/11_deepin_ab_recovery.cfg"
	abRecoveryGrubCfg12File = "/etc/default/grub.d/12_deepin_ab_recovery.cfg"
	abRecoveryFile          = "/usr/lib/deepin-daemon/ab-recovery"
	ddeWelcomeFile          = "/usr/lib/deepin-daemon/dde-welcome"
	abKernelBackupDir       = "/boot/kernel-backup/"
	backupPartitionMarkFile = ".deepin-ab-recovery-backup"
	defaultHospiceDir       = "/usr/share/deepin-ab-recovery/hospice/"
)

var globalNoGrubMkconfig bool
var globalUsePmonBios bool

var globalArch string
var globalGrubCfgFile = "/boot/grub/grub.cfg"
var globalPmonCfgFile = "/boot/boot/boot.cfg"
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
	fixBackup      bool
	printShHideOs  bool
}

type extraDir struct {
	originDir       string   // 需要备份的文件夹
	hospiceChildDir string   // 为空时,和base(originDir)一致
	specifiedFiles  []string // 该切片内只能存放originDir中的文件或文件夹名
}

var _extraDirs = []extraDir{
	{
		originDir: "/var/lib/systemd",
	},
	{
		originDir: "/var/uos",
		specifiedFiles: []string{
			"os-license",
		},
	},
}

const backupRecordPath = "/var/lib/deepin-ab-recovery/record.json"

var _lastBackUpRecord map[string]string
var _currentBackUpRecord map[string]string

var _renameFailedMsgRegexp = regexp.MustCompile(`rsync: rename "([0-9a-zA-Z/+.=-]+)" -> "([0-9a-zA-Z/+.=-]+)": Operation not permitted`)
var _delFailedMsgRegexp = regexp.MustCompile(`rsync: delete_file: unlink[(]([0-9a-zA-Z/+.=-]+)[)] failed: Operation not permitted`)

func init() {
	flag.BoolVar(&options.noRsync, "no-rsync", false, "")
	flag.BoolVar(&options.noGrubMkconfig, "no-grub-mkconfig", false, "")
	flag.BoolVar(&options.grubMenuEn, "grub-menu-en", false, "grub menu entry use english")
	flag.BoolVar(&options.fixBackup, "fix-backup", false, "Fix bugs in backup partition")
	flag.BoolVar(&options.printShHideOs, "print-sh-hide-os", false,
		"print the shell script to hide the backup OS")
	flag.StringVar(&options.arch, "arch", "", "")
	flag.StringVar(&options.grubCfgFile, "grub-cfg", "", "")
	flag.StringVar(&options.bootDir, "boot", "", "")
}

// 此函数用于 /etc/default/grub.d/12_deepin_ab_recovery.cfg 脚本
func printShHideOs() (exitCode int) {
	logger.RemoveBackendConsole() // 避免输出日志到标准输出
	setLogEnv(logEnvGrubMkconfig)
	devices, err := runOsProber()
	if err != nil {
		logWarningf("run os-prober error: %v", err)
		exitCode = 1
		return
	}
	for _, device := range devices {
		is, err := isBackupDevice(device)
		if err != nil {
			logWarningf("isBackupDevice error: %v", err)
			continue
		}
		if !is {
			continue
		}
		uuid, err := getDeviceUuid(device)
		if err != nil {
			logWarningf("get device uuid failed: %v", err)
			exitCode = 2
			return
		}
		fmt.Printf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n", uuid, device)
		return
	}
	// 没有找到备份分区的情况,默认将rootb分区作为备份分区
	uuid, err := getUuidByLabel("rootb")
	if err != nil {
		logWarningf("get rootb uuid error: %v", err)
		exitCode = 3
		return
	}
	mountPoint, err := getMountPointByLabel("rootb")
	if err != nil {
		logWarningf("get rootb mountPoint error: %v", err)
		exitCode = 4
		return
	}
	if strings.TrimSpace(mountPoint) == "/" {
		logWarningf("Cannot use rootb as a backup partition")
		exitCode = 5
		return
	}
	device, err := getDeviceByUuid(uuid)
	if err != nil {
		logWarningf("get backup device by backup uuid error: %v", err)
		exitCode = 6
		return
	}
	fmt.Printf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n", uuid, device)
	return
}

func isBackupDevice(device string) (bool, error) {
	dir := "/deepin-ab-recovery-isBackupDevice"
	umount, err := mountDevice(device, dir)
	if umount != nil {
		defer umount()
	}
	if err != nil {
		return false, err
	}

	_, err = os.Stat(filepath.Join(dir, backupPartitionMarkFile))
	return err == nil, nil
}

func mountDevice(device, dir string) (fn func(), err error) {
	fn = func() {
		umountDeleteDir(dir)
	}
	mounted, err := isMounted(dir)
	if err != nil {
		return
	}
	if mounted {
		err = exec.Command("umount", dir).Run()
		if err != nil {
			err = xerrors.Errorf("failed to unmount %s: %w", dir, err)
			return
		}
	}

	err = os.Mkdir(dir, 0755)
	if err != nil && !os.IsExist(err) {
		return
	}

	err = exec.Command("mount", device, dir).Run()
	if err != nil {
		err = xerrors.Errorf("failed to mount device %q to dir %q: %w",
			device, dir, err)
	}
	return
}

func umountDeleteDir(dir string) {
	err := exec.Command("umount", dir).Run()
	if err != nil {
		logWarningf("failed to umount directory %q: %v", dir, err)
	}

	err = os.Remove(dir)
	if err != nil {
		logWarningf("failed to remove unmounted directory: %v", err)
	}
}

func main() {
	flag.Parse()
	if options.printShHideOs {
		exitCode := printShHideOs()
		os.Exit(exitCode)
	}

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
	}

	if options.noGrubMkconfig {
		globalNoGrubMkconfig = true
	}
	if options.grubCfgFile != "" {
		globalGrubCfgFile = options.grubCfgFile
	}

	if options.grubMenuEn || isArchMips() || isArchArm() {
		globalGrubMenuEn = true
	}

	if options.bootDir != "" {
		globalBootDir = filepath.Clean(options.bootDir)
	}
	globalKernelBackupDir = filepath.Join(globalBootDir, "deepin-ab-recovery")

	if options.fixBackup {
		err := fixBackup()
		if err != nil {
			logger.Fatal("failed to fix backup error:", err)
		}
		return
	}

	logger.Debug("arch:", globalArch)
	logger.Debug("noGrubMkConfig:", globalNoGrubMkconfig)
	logger.Debug("usePmonBios:", globalUsePmonBios)
	logger.Debug("bootDir:", globalBootDir)
	logger.Debug("grubCfgFile:", globalGrubCfgFile)
	logger.Debug("pmonCfgFile:", globalPmonCfgFile)
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
		"/media", "/tmp", "/proc", "/sys", "/dev", "/run",
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
	osReleaseInfo, oserr := runOsRelease()
	lsbReleaseInfo, err := runLsbRelease()
	if err != nil {
		logger.Warning("failed to run lsb-release:", err)
	} else {
		if oserr != nil {
			osVersion = lsbReleaseInfo[lsbReleaseKeyRelease]
			osDesc = lsbReleaseInfo[lsbReleaseKeyDesc]
		} else {
			systemName := osReleaseInfo[osSystemName]
			majorVersion := osReleaseInfo[osMajorVersion]
			EditName := osReleaseInfo[osEditionName]
			osDesc = systemName + " " + majorVersion + " " + EditName
			osVersion = majorVersion
		}
	}

	now := time.Now()
	cfg.Time = &now
	cfg.Version = osVersion
	err = cfg.save(configFile)
	if err != nil {
		return xerrors.Errorf("failed to save config file %q: %w", configFile, err)
	}

	initBackUpRecord(backupRecordPath, defaultHospiceDir)
	recoverDeprecatedFilesOrDirs(backupRecordPath, false)
	err = updateBackUpRecordFile(backupRecordPath)
	if err != nil {
		logger.Warning(err)
		return err
	}
	backupExtra()
	errMsg, err := runRsync(tmpExcludeFile)
	if err != nil {
		allMatchedString := _renameFailedMsgRegexp.FindAllStringSubmatch(errMsg, -1)
		for _, matchString := range allMatchedString {
			if len(matchString) == 3 {
				tempFilePath := matchString[1]
				destFilePath := matchString[2]
				if strings.Contains(filepath.Base(tempFilePath), filepath.Base(destFilePath)) {
					err := exec.Command("chattr", "-i", filepath.Join(backupMountPoint, destFilePath)).Run()
					if err != nil {
						logger.Warning(err)
						continue
					}
				}
			}
		}
		allMatchedString = _delFailedMsgRegexp.FindAllStringSubmatch(errMsg, -1)
		for _, matchString := range allMatchedString {
			if len(matchString) == 2 {
				err := exec.Command("chattr", "-i", filepath.Join(backupMountPoint, matchString[1])).Run()
				if err != nil {
					logger.Warning(err)
					continue
				}
			}
		}
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

	err = ioutil.WriteFile(filepath.Join(backupMountPoint, backupPartitionMarkFile), nil, 0644)
	if err != nil {
		return xerrors.Errorf("failed to write backup partition mark file: %w", err)
	}

	// generate bootloader config
	err = writeBootloaderCfgBackup(backupUuid, backupDevice, osDesc, kFiles, now, envVars)
	if err != nil {
		return xerrors.Errorf("failed to write bootloader cfg: %w", err)
	}

	return nil
}

// 备份不在根分区的额外文件夹，比如实际上在 /data 分区的 /var/lib/systemd 文件夹。
func backupExtra() {
	for origin, backupPath := range _currentBackUpRecord {
		isSym, err := isSymlink(origin)
		if err != nil {
			logger.Warningf("isSymlink %q failed: %v", origin, err)
			continue
		}
		if isSym {
			continue
		}
		err = os.MkdirAll(filepath.Dir(backupPath), 0755)
		if err != nil {
			logger.Warningf("make backup dir failed: %v", err)
			continue
		}
		err = os.RemoveAll(backupPath)
		if err != nil {
			logger.Warningf("remove dir failed: %v", err)
			continue
		}
		err = exec.Command("cp", "-a", origin, backupPath).Run()
		if err != nil {
			logger.Warningf("run cp command failed: %v", err)
			continue
		}
	}
}

func runRsync(excludeFile string) (string, error) {
	var errBuffer bytes.Buffer
	if options.noRsync {
		logger.Debug("skip run rsync")
		return "", nil
	}

	logger.Debug("run rsync...")
	var rsyncArgs []string
	if logger.GetLogLevel() == log.LevelDebug {
		rsyncArgs = append(rsyncArgs, "-v")
	}
	rsyncArgs = append(rsyncArgs, "-X", "-x", "-a", "--delete-after",
		"--exclude-from="+excludeFile,
		"/", backupMountPoint+"/")

	cmd := exec.Command("rsync", rsyncArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = &errBuffer
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	err := cmd.Run()
	return errBuffer.String(), err
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
	if kFiles.initrd != "" {
		initrdBackup := filepath.Join(globalKernelBackupDir, filepath.Base(kFiles.initrd))
		err = utils.CopyFile(kFiles.initrd, initrdBackup)
		if err != nil {
			return
		}
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
	// allow initrd not found

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

func fixBackup() error {
	var cfg Config
	err := loadConfig(configFile, &cfg)
	if err != nil {
		if os.IsNotExist(err) {
			// 不存在配置文件，不能备份，立即返回
			return nil
		}
		return xerrors.Errorf("load config: %w", err)
	}
	backupUuid := cfg.Backup
	backupDevice, err := getDeviceByUuid(backupUuid)
	if err != nil {
		return xerrors.Errorf("get backup device by backup uuid: %w", err)
	}

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
			logger.Warning("failed to umount backup directory:", err)
		}
	}()

	// 替换备份盘中的恢复程序
	backupPartitionAbRecoveryFile := filepath.Join(backupMountPoint, abRecoveryFile)
	_, err = os.Stat(filepath.Dir(backupPartitionAbRecoveryFile))
	if err != nil {
		if os.IsNotExist(err) {
			// 目前备份分区为空，不修正
			return nil
		}
		return xerrors.Errorf("stat dir: %w", err)
	}

	err = utils.CopyFile(abRecoveryFile, backupPartitionAbRecoveryFile)
	if err != nil {
		return err
	}
	err = utils.CopyFile(abRecoveryGrubCfg12File, filepath.Join(backupMountPoint, abRecoveryGrubCfg12File))
	if err != nil {
		return err
	}
	// 暂时屏蔽真dde-welcome运行
	backupDDEWelcomeFile := filepath.Join(backupMountPoint, ddeWelcomeFile)
	backupDDEWelcomeFileInfo, err := os.Stat(backupDDEWelcomeFile)
	if err == nil && backupDDEWelcomeFileInfo != nil &&
		backupDDEWelcomeFileInfo.Size() > 100 {

		err = os.Rename(backupDDEWelcomeFile, backupDDEWelcomeFile+".save")
		if err != nil {
			return err
		}
		var content = []byte("#!/bin/sh\nexec /usr/bin/true")
		err = ioutil.WriteFile(backupDDEWelcomeFile, content, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}

func restore(cfg *Config, envVars []string) error {
	currentDevice, err := getDeviceByUuid(cfg.Current)
	if err != nil {
		return xerrors.Errorf("failed to get device by uuid %q: %w", cfg.Current, err)
	}

	_, err = os.Stat(ddeWelcomeFile + ".save")
	if err == nil {
		err = os.Rename(ddeWelcomeFile+".save", ddeWelcomeFile)
		if err != nil {
			logger.Warning("failed to restore dde-welcome:", err)
		}
	}

	// 创建备份文件夹
	err = os.MkdirAll(abKernelBackupDir, 0755)
	if err != nil {
		return xerrors.Errorf("failed to make dir %s: %w", abKernelBackupDir, err)
	}

	// 移动内核文件到/boot/kernel-backup/
	fileInfoList, err := ioutil.ReadDir(globalBootDir)
	if err != nil {
		return xerrors.Errorf("failed to read dir %s: %w", globalBootDir, err)
	}
	prefixes := []string{"vmlinuz-", "vmlinux-", "kernel-", "initrd"}
	for _, fix := range prefixes {
		for _, info := range fileInfoList {
			if info.IsDir() {
				continue
			}
			if strings.Contains(info.Name(), fix) {
				err = os.Rename(filepath.Join(globalBootDir, info.Name()),
					filepath.Join(abKernelBackupDir, info.Name()))
				if err != nil {
					logger.Warning("backup kernel failed:", info.Name(), err)
				}
			}
		}
	}

	// 将/boot/deepin-ab-recovery文件内核文件移动到 /boot
	fileInfoList, err = ioutil.ReadDir(globalKernelBackupDir)
	if err != nil {
		return xerrors.Errorf("failed to read dir %s: %w", globalKernelBackupDir, err)
	}

	for _, info := range fileInfoList {
		err = utils.CopyFile(filepath.Join(globalKernelBackupDir, info.Name()), filepath.Join(globalBootDir, info.Name()))
		if err != nil {
			logger.Warning("copy recovery file failed:", err)
			return err
		}
	}

	err = os.RemoveAll(globalKernelBackupDir)
	if err != nil {
		logger.Warning("Remove dir failed:", globalKernelBackupDir, err)
	}

	err = writeBootloaderCfgRestore(cfg.Current, currentDevice, cfg.Backup, envVars)
	if err != nil {
		return xerrors.Errorf("failed to write grub cfg: %w", err)
	}
	initBackUpRecord(backupRecordPath, defaultHospiceDir)
	recoverDeprecatedFilesOrDirs(backupRecordPath, true)
	restoreExtra()
	// swap current and backup
	cfg.Current, cfg.Backup = cfg.Backup, cfg.Current
	cfg.Time = nil
	cfg.Version = ""
	err = cfg.save(configFile)
	if err != nil {
		return xerrors.Errorf("failed to save config file %q: %w", configFile, err)
	}

	// 还原时，对需要隐藏的分区进行处理: 将备份分区进行隐藏，并解除挂载
	var rulesPaths = []string{
		"/etc/udev/rules.d/80-udisks2.rules",
		"/etc/udev/rules.d/80-udisks-installer.rules",
	}
	foundRules := false

	rootDisk, err := getPathDisk("/")
	if err != nil {
		return xerrors.Errorf("failed to get root disk: %w", err)
	}

	labelUuidMap, err := getLabelUuidMap(rootDisk)
	if err != nil {
		return xerrors.Errorf("failed to get label uuid map: %w", err)
	}
	backupDevice, err := getDeviceByUuid(cfg.Backup)
	if err != nil {
		logger.Warning(err)
		return err
	}
	backupLabel, err := getDeviceLabel(backupDevice)
	if err != nil {
		logger.Warning(err)
		return err
	}
	for _, rulesPath := range rulesPaths {
		_, err = os.Stat(rulesPath)
		if err == nil {
			err = modifyRules(rulesPath, labelUuidMap, cfg.Backup, cfg.Current, backupLabel)
			if err != nil {
				return xerrors.Errorf("failed to modify rules: %w", err)
			}
			foundRules = true
		}
	}
	if !foundRules {
		logger.Warning("not found 80-udisks-installer.rules or 80-udisks2.rules")
	} else {
		err = reloadUdev() // 重载udev的rules,让rules的修改生效
		if err != nil {
			logger.Warning(err)
			return err
		}
		mountDir, err := getMountPointByLabel(strings.ToLower(strings.TrimSpace(backupLabel)))
		if err != nil {
			logger.Warning(err)
		} else {
			umountDeleteDir(mountDir)
		}
	}
	// end

	err = os.Remove(filepath.Join("/", backupPartitionMarkFile))
	if err != nil && !os.IsNotExist(err) {
		return xerrors.Errorf("failed to delete backup partition mark file: %w", err)
	}

	return nil
}

// 回退不在根分区的额外文件夹，实际上是通过创建软链接完成的。
// 如果已经是软链接了，则不需要处理。
func restoreExtra() {
	for origin, backupPath := range _lastBackUpRecord {
		isSym, err := isSymlink(origin)
		if err != nil {
			logger.Warningf("isSymlink %q failed: %v", origin, err)
			continue
		}
		if isSym {
			continue
		}
		_, err = os.Stat(backupPath)
		if err != nil {
			logger.Warningf("stat backup path failed: %v", err)
			continue
		}
		err = os.RemoveAll(origin)
		if err != nil {
			logger.Warningf("remove origin dir failed: %v", err)
			continue
		}
		err = os.Symlink(backupPath, origin)
		if err != nil {
			logger.Warningf("create symlink for %q failed: %v", origin, err)
			continue
		}
	}
}

func writeBootloaderCfgRestore(currentUuid, currentDevice, backupUuid string, envVars []string) error {
	if globalUsePmonBios {
		return writePmonCfgRestore(backupUuid)
	}

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

func writePmonCfgRestore(uuid string) error {
	cfg, err := pmoncfg.ParsePmonCfgFile(globalPmonCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to parse grub cfg file: %w", err)
	}

	cfg.RemoveRecoveryMenuEntries()
	err = cfg.ReplaceRootUuid(uuid)
	if err != nil {
		return xerrors.Errorf("failed to replace root uuid: %w", err)
	}

	err = cfg.Save(globalPmonCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to save grub cfg file: %w", err)
	}
	return nil
}

func writeBootloaderCfgBackup(backupUuid, backupDevice, osDesc string,
	kFiles *kernelFiles, backupTime time.Time, envVars []string) error {
	if globalGrubMenuEn {
		envVars = []string{"LANG=en_US.UTF-8", "LANGUAGE=en_US"}
	}

	if globalUsePmonBios {
		return writePmonCfgBackup(backupUuid, osDesc, kFiles, backupTime)
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
	buf.WriteString(varPrefix + "BACKUP_DEVICE=" + backupDevice + "\n")
	buf.WriteString(varPrefix + "BACKUP_UUID=" + backupUuid + "\n")
	buf.WriteString(fmt.Sprintf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n",
		backupUuid, backupDevice))
	buf.WriteString(varPrefix + "LINUX=\"" + filepath.Join(globalKernelBackupDir,
		filepath.Base(kFiles.linux)) + "\"\n")
	if kFiles.initrd != "" {
		buf.WriteString(varPrefix + "INITRD=\"" + filepath.Base(kFiles.initrd) + "\"\n")
	}
	buf.WriteString(varPrefix + "OS_DESC=\"" + osDesc + "\"\n")
	buf.WriteString(varPrefix + "BACKUP_TIME=" + strconv.FormatInt(backupTime.Unix(), 10) + "\n")

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

func writePmonCfgBackup(backupUuid string, osDesc string, kFiles *kernelFiles, backupTime time.Time) error {
	cfg, err := pmoncfg.ParsePmonCfgFile(globalPmonCfgFile)
	if err != nil {
		return err
	}

	cfg.RemoveRecoveryMenuEntries()

	menuText := getRollbackMenuTextForceEn(osDesc, backupTime)
	dir := strings.TrimPrefix(globalKernelBackupDir, globalBootDir)
	linux := filepath.Join(dir, filepath.Base(kFiles.linux))
	initrd := filepath.Join(dir, filepath.Base(kFiles.initrd))
	cfg.AddRecoveryMenuEntry(menuText, backupUuid, linux, initrd)

	err = cfg.Save(globalPmonCfgFile)
	if err != nil {
		return xerrors.Errorf("failed to save pmon cfg file: %w", err)
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
	cmd = exec.Command("date", "+%Y/%-m/%-d %T", "-d", "@"+backupTs)
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

// 根据备份记录,还原修改
func recoverDeprecatedFilesOrDirs(recordPath string, isRestore bool) {
	const oldBackupPath = "/usr/share/deepin-ab-recovery/hospice/uos"
	if !isExist(recordPath) { // 如果不存在该文件,则为兼容旧版本时使用
		// 兼容 /var/uos文件夹备份改为 /var/uos/os-license文件备份
		// 处理软链接和非软链接两种情况
		isSym, err := isSymlink("/var/uos")
		if err != nil {
			logger.Warningf("isSymlink %q failed: %v", "/var/uos", err)
			return
		}
		if isSym {
			err := os.RemoveAll("/var/uos")
			if err != nil {
				logger.Warningf("remove origin dir failed: %v", err)
				return
			}
			err = exec.Command("mv", oldBackupPath, "/var").Run()
			if err != nil {
				logger.Warningf("mv backup dir to origin dir failed: %v", err)
				return
			}
		} else {
			if isRestore {
				err := exec.Command("mv", filepath.Join(oldBackupPath, "os-license"), "/var/uos", "-f").Run() // 将os-license文件还原至备份时候的状态
				if err != nil {
					logger.Warningf("only restore os-license failed: %v", err)
					return
				}
			}
			err := os.RemoveAll(oldBackupPath)
			if err != nil {
				logger.Warningf("remove origin dir failed: %v", err)
				return
			}
		}
	}

	for originPath, backupPath := range _lastBackUpRecord {
		// 判断新旧版本备份内容是否存在差异
		if currentBackupPath, ok := _currentBackUpRecord[originPath]; ok && backupPath == currentBackupPath {
			continue
		}
		// 恢复之前的备份
		isSym, err := isSymlink(originPath)
		if err != nil {
			logger.Warningf("isSymlink %q failed: %v", originPath, err)
			continue
		}
		if isSym {
			err = os.RemoveAll(originPath)
			if err != nil {
				logger.Warningf("remove origin dir failed: %v", err)
				continue
			}
			err = exec.Command("cp", "-a", backupPath, originPath).Run()
			if err != nil {
				logger.Warningf("run cp command failed: %v", err)
				continue
			}
		}
		err = os.RemoveAll(backupPath)
		if err != nil {
			logger.Warningf("remove backup file or dir failed: %v", err)
			continue
		}
	}
	return
}

// 更新记录备份项的文件
func updateBackUpRecordFile(path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}
	data, err := json.Marshal(_currentBackUpRecord)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, data, 0644)
}

// 初始化_lastBackUpRecord和_currentBackUpRecord
func initBackUpRecord(recordPath, hospice string) {
	// 最新备份配置
	_currentBackUpRecord = make(map[string]string)
	for _, item := range _extraDirs {
		if item.specifiedFiles != nil {
			var hospiceChildDir string
			if item.hospiceChildDir == "" {
				hospiceChildDir = filepath.Base(item.originDir)
			} else {
				hospiceChildDir = item.hospiceChildDir
			}
			for _, file := range item.specifiedFiles {
				_currentBackUpRecord[filepath.Join(item.originDir, file)] = filepath.Join(hospice, hospiceChildDir, file)
			}
		} else {
			var hospiceChildDir string
			if item.hospiceChildDir == "" {
				hospiceChildDir = filepath.Base(item.originDir)
			} else {
				hospiceChildDir = item.hospiceChildDir
			}
			_currentBackUpRecord[item.originDir] = filepath.Join(hospice, hospiceChildDir)
		}
	}
	// 备份记录
	_lastBackUpRecord = make(map[string]string)
	content, err := ioutil.ReadFile(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info(err)
		} else {
			logger.Warningf("read %s file failed: %v", recordPath, err)
		}
		return
	}
	err = json.Unmarshal(content, &_lastBackUpRecord)
	if err != nil {
		logger.Warningf("unmarshal %s file to json failed: %v", recordPath, err)
		return
	}
}
