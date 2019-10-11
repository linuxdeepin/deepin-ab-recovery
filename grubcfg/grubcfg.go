package grubcfg

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"golang.org/x/xerrors"
)

type Line struct {
	value string
}

type MenuEntry struct {
	head  string
	items []string
}

type GrubCfg struct {
	items []GrubCfgItem
}

type GrubCfgItem interface {
	String() string
	Length() int
}

func (l Line) String() string {
	return l.value + "\n"
}

func (l Line) Length() int {
	return len(l.value) + 1
}

func (me *MenuEntry) String() string {
	length := me.Length()
	buf := bytes.NewBuffer(make([]byte, 0, length))
	buf.WriteString(me.head)
	buf.WriteByte('\n')
	for _, item := range me.items {
		buf.WriteString(item)
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.String()
}

func (me *MenuEntry) Length() int {
	length := len(me.head) + 3
	for _, line := range me.items {
		length += len(line) + 1
	}
	return length
}

func (cfg *GrubCfg) RemoveRecoveryMenuEntries() {
	var items []GrubCfgItem
	for _, item := range cfg.items {
		me, ok := item.(*MenuEntry)
		if ok {
			if strings.Contains(me.head, " --class ab-recovery ") {
				// remove it
			} else {
				items = append(items, item)
			}
		} else {
			items = append(items, item)
		}
	}
	cfg.items = items
}

var regRootUUID = regexp.MustCompile(`root=UUID=[0-9a-fA-F\-]+`)

func (cfg *GrubCfg) ReplaceRootUuid(uuid string) error {
	for _, item := range cfg.items {
		me, ok := item.(*MenuEntry)
		if ok {
			if strings.Contains(me.head, "Recovery") {
				continue
			}

			for idx, item := range me.items {
				line := strings.TrimSpace(item)
				if strings.HasPrefix(line, "linux") && regRootUUID.MatchString(line) {
					me.items[idx] = regRootUUID.ReplaceAllString(item, "root=UUID="+uuid)
					return nil
				}
			}
		}
	}

	return xerrors.New("not found replace target")
}

/*
menuentry 'Deepin  15.5 sp2 sw421' --class gnu-linux --class gnu --class os{
echo "装载中，请耐心等待……"
set boot=(${root})/boot/
linux.boot ${boot}/initrd.img-4.4.15-aere-deepin
echo "装载 boot.img 成功"
linux.console ${boot}/bootloader.bin
echo "装载 bootloader.bin 成功"
linux.vmlinux ${boot}/vmlinuz-4.4.15-aere-deepin  root=UUID=91f9e990-4958-4a32-a741-41da2ef4218c net.ifnames=0 loglevel=0 vga=current rd.systemd.show_status=false rd.udev.log-priority=3 quiet  video=swichfb:1280x1024-32@60
echo "装载 vmlinux1 成功"
echo "开始执行……"
boot
}

*/
func (cfg *GrubCfg) AddRecoveryMenuEntrySw(menuText, rootUuid, linux, initrd string) {
	cfg.items = append(cfg.items, &MenuEntry{
		head: fmt.Sprintf("menuentry '%s' --class ab-recovery {", menuText),
		items: []string{
			`echo "装载中，请耐心等待……"`,
			`set boot=(${root})/boot/`,
			fmt.Sprintf("linux.boot ${boot}/%s", initrd),
			`echo "装载 boot.img 成功"`,
			"linux.console ${boot}/bootloader.bin",
			fmt.Sprintf("linux.vmlinux ${boot}/%s  root=UUID=%s net.ifnames=0 loglevel=0 vga=current rd.systemd.show_status=false rd.udev.log-priority=3 quiet  video=swichfb:1280x1024-32@60",
				linux, rootUuid),
			`echo "装载 vmlinux 成功"`,
			`echo "开始执行……"`,
			"boot",
		},
	})
}

func (cfg *GrubCfg) AddRecoveryMenuEntryMips(menuText, rootUuid, linux, initrd string) {
	cfg.items = append(cfg.items, &MenuEntry{
		head: fmt.Sprintf("menuentry '%s' --class ab-recovery {", menuText),
		items: []string{
			fmt.Sprintf("linux ${prefix}/%s console=tty loglevel=0 quiet splash locales=zh_CN.UTF-8  root=UUID=%s", linux, rootUuid),
			fmt.Sprintf("initrd ${prefix}/%s", initrd),
			"boot",
		},
	})
}

func (cfg *GrubCfg) toBytes() []byte {
	length := 0
	for _, item := range cfg.items {
		length += item.Length()
	}
	buf := bytes.NewBuffer(make([]byte, 0, length))
	for _, item := range cfg.items {
		buf.WriteString(item.String())
	}
	return buf.Bytes()
}

func (cfg *GrubCfg) Save(filename string) error {
	content := cfg.toBytes()
	return ioutil.WriteFile(filename, content, 0644)
}

func ParseGrubCfgFile(filename string) (*GrubCfg, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, xerrors.Errorf("failed to read file: %w", err)
	}

	var cfg GrubCfg
	br := bytes.NewReader(content)
	scanner := bufio.NewScanner(br)

	var currentMenuEntry *MenuEntry
	for scanner.Scan() {
		line0 := scanner.Bytes()
		line := bytes.TrimSpace(line0)
		if bytes.HasPrefix(line, []byte("menuentry ")) &&
			bytes.HasSuffix(line, []byte("{")) {
			// 开始 menuentry 定义

			currentMenuEntry = &MenuEntry{
				head: string(line0),
			}
			cfg.items = append(cfg.items, currentMenuEntry)
		} else if string(line) == "}" {
			// 结束 menuentry 定义
			currentMenuEntry = nil
		} else {
			if currentMenuEntry == nil {
				cfg.items = append(cfg.items, Line{string(line0)})
			} else {
				currentMenuEntry.items = append(currentMenuEntry.items, string(line0))
			}
		}
	}
	err = scanner.Err()
	if err != nil {
		return nil, xerrors.Errorf("scanner error: %w", err)
	}
	return &cfg, nil
}
