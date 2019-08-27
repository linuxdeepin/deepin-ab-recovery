package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type Config struct {
	Current string
	Backup  string
}

func loadConfig(filename string) (*Config, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var c Config
	err = json.Unmarshal(content, &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) save(filename string) error {
	content, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filename, content, 0644)
}

func (c *Config) check() error {
	if !hasDiskDevice(c.Current) {
		return fmt.Errorf("not found current disk %q", c.Current)
	}

	if !hasDiskDevice(c.Backup) {
		return fmt.Errorf("not found backup disk %q", c.Backup)
	}

	return nil
}

const (
	configFile       = "/etc/deepin/recovery.json"
	backupMountPoint = "/deepin-recovery-backup"
	grubCfgFile      = "/etc/default/grub.d/11_deepin_recovery.cfg"
)

func main() {
	log.SetFlags(log.Lshortfile)

	cfg, err := loadConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}
	err = cfg.check()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("current:", cfg.Current)
	log.Println("backup:", cfg.Backup)

	if len(os.Args) != 2 {
		os.Exit(1)
	}

	arg := os.Args[1]
	if arg == "backup" {
		err := backup(cfg)
		if err != nil {
			log.Fatal(err)
		}
	} else if arg == "restore" {
		err := restore(cfg)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func backup(cfg *Config) error {
	rootUuid, err := getRootUuid()
	if err != nil {
		return err
	}
	log.Println("root uuid:", rootUuid)

	if rootUuid != cfg.Current {
		return errors.New("rootUuid is not current")
	}

	backupUuid := cfg.Backup
	backupDevice, err := getDeviceByUuid(backupUuid)
	if err != nil {
		return err
	}

	log.Println("backup device:", backupDevice)

	err = os.Mkdir(backupMountPoint, 0755)
	if err != nil {
		return err
	}
	defer func() {
		err = os.Remove(backupMountPoint)
		if err != nil {
			log.Println("WARN:", err)
		}
	}()

	err = exec.Command("mount", backupDevice, backupMountPoint).Run()
	if err != nil {
		return err
	}
	defer func() {
		err := exec.Command("umount", backupMountPoint).Run()
		if err != nil {
			log.Println("WARN:", err)
		}
	}()

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
			log.Println("WARN:", err)
		}
	}()

	log.Println("run rsync...")
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

	cmd = exec.Command("update-grub")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func restore(cfg *Config) error {
	rootUuid, err := getRootUuid()
	if err != nil {
		return err
	}
	log.Println("root uuid:", rootUuid)

	if rootUuid != cfg.Backup {
		return errors.New("rootUuid is not backup")
	}

	currentDevice, err := getDeviceByUuid(cfg.Current)
	if err != nil {
		return err
	}

	err = writeGrubCfgRestore(grubCfgFile, cfg.Current, currentDevice)
	if err != nil {
		return err
	}

	cmd := exec.Command("update-grub")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
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
