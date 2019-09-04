package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"pkg.deepin.io/lib/dbusutil"
	"pkg.deepin.io/lib/log"
)

var logger = log.NewLogger("ab-recovery/system")

const (
	configFile       = "/etc/deepin/ab-recovery.json"
	backupMountPoint = "/deepin-ab-recovery-backup"
	grubCfgFile      = "/etc/default/grub.d/11_deepin_ab_recovery.cfg"
)

func main() {
	service, err := dbusutil.NewSystemService()
	if err != nil {
		logger.Fatal(err)
	}

	m := newManager(service)
	err = service.Export(dbusPath, m)
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

func backup(backupUuid string) error {
	backupDevice, err := getDeviceByUuid(backupUuid)
	if err != nil {
		return err
	}

	logger.Debug("backup device:", backupDevice)

	err = os.Mkdir(backupMountPoint, 0755)
	if err != nil {
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

	// TODO: 正确处理 /boot
	skipDirs := []string{
		"/sys", "/dev", "/proc", "/run", "/media", "/home", "/tmp", "/boot",
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

	logger.Debug("run rsync...")
	cmd := exec.Command("rsync", "-va", "--delete-after", "--exclude-from="+tmpExcludeFile,
		"/", backupMountPoint+"/")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	for _, dir := range skipDirs {
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
		return err
	}

	// generate grub config
	err = writeGrubCfgBackup(grubCfgFile, backupUuid, backupDevice)
	if err != nil {
		return err
	}

	err = runUpdateGrub()
	if err != nil {
		return err
	}

	return nil
}

func runUpdateGrub() error {
	cmd := exec.Command("grub-mkconfig", "-o", "/boot/grub/grub.cfg")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func restore(cfg *Config) error {
	currentDevice, err := getDeviceByUuid(cfg.Current)
	if err != nil {
		return err
	}

	err = writeGrubCfgRestore(grubCfgFile, cfg.Current, currentDevice)
	if err != nil {
		return err
	}

	err = runUpdateGrub()
	if err != nil {
		return err
	}

	// swap current and backup
	cfg.Current, cfg.Backup = cfg.Backup, cfg.Current
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

func writeGrubCfgBackup(filename, backupUuid, backupDevice string) error {
	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("DEEPIN_RECOVERY_BACKUP_UUID=" + backupUuid + "\n")
	buf.WriteString(fmt.Sprintf("GRUB_OS_PROBER_SKIP_LIST=\"$GRUB_OS_PROBER_SKIP_LIST %s@%s\"\n",
		backupUuid, backupDevice))

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
