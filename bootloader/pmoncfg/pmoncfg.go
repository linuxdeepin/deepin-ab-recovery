// SPDX-FileCopyrightText: 2022 UnionTech Software Technology Co., Ltd.
//
// SPDX-License-Identifier: GPL-3.0-or-later

package pmoncfg

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"

	bootloader ".."
)

type menuEntry struct {
	title  string
	kernel string
	initrd string
	args   string
}

type PmonCfg struct {
	defaultItem int
	timeout     int
	showMenu    int
	items       []*menuEntry
}

const recoveryTitleSuffix = " # ab-recovery"
const kernelPathPrefix = "/dev/fs/ext2@wd0"

func ParsePmonCfgFile(filename string) (*PmonCfg, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &PmonCfg{}

	var currentMenuEntry *menuEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		if strings.HasPrefix(line, "default") {
			cfg.defaultItem, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "default")))
			if err != nil {
				return nil, err
			}
			continue
		}

		if strings.HasPrefix(line, "timeout") {
			cfg.timeout, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "timeout")))
			if err != nil {
				return nil, err
			}
			continue
		}

		if strings.HasPrefix(line, "showmenu") {
			cfg.showMenu, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "showmenu")))
			if err != nil {
				return nil, err
			}
			continue
		}

		if strings.HasPrefix(line, "title") {
			if currentMenuEntry != nil {
				cfg.items = append(cfg.items, currentMenuEntry)
			}

			currentMenuEntry = &menuEntry{
				title: strings.TrimSpace(strings.TrimPrefix(line, "title")),
			}

			continue
		}

		if currentMenuEntry == nil {
			return nil, errors.New("failed to parse pmon cfg: menu entries must start with title")
		}

		if strings.HasPrefix(line, "kernel") {
			currentMenuEntry.kernel = strings.TrimSpace(strings.TrimPrefix(line, "kernel"))
			continue
		}

		if strings.HasPrefix(line, "initrd") {
			currentMenuEntry.initrd = strings.TrimSpace(strings.TrimPrefix(line, "initrd"))
			continue
		}

		if strings.HasPrefix(line, "args") {
			currentMenuEntry.args = strings.TrimSpace(strings.TrimPrefix(line, "args"))
			continue
		}
	}

	if currentMenuEntry != nil {
		cfg.items = append(cfg.items, currentMenuEntry)
	}

	return cfg, nil
}

func (cfg *PmonCfg) RemoveRecoveryMenuEntries() {
	newEntries := make([]*menuEntry, 0, len(cfg.items))
	for _, item := range cfg.items {
		if !strings.HasSuffix(item.title, recoveryTitleSuffix) {
			newEntries = append(newEntries, item)
		}
	}

	cfg.items = newEntries
}

func (cfg *PmonCfg) AddRecoveryMenuEntry(menuText, rootUuid, linux, initrd string) {
	cfg.items = append(cfg.items, &menuEntry{
		title:  menuText + recoveryTitleSuffix,
		kernel: path.Join(kernelPathPrefix, linux),
		initrd: path.Join(kernelPathPrefix, initrd),
		args:   fmt.Sprintf("root=UUID=%s console=tty loglevel=0 quiet splash", rootUuid),
	})
}

func (cfg *PmonCfg) ReplaceRootUuid(uuid string) error {
	var result bool
	for _, item := range cfg.items {
		if strings.HasSuffix(item.title, recoveryTitleSuffix) {
			continue
		}

		result = true
		item.args = bootloader.RegRootUUID.ReplaceAllString(item.args, "root=UUID="+uuid)
	}

	if result {
		return nil
	}

	return errors.New("not found replace target")
}

func (cfg *PmonCfg) Save(filename string) error {
	template := `
title %s
        kernel %s
        initrd %s
        args %s
`

	var content string
	content += "default " + strconv.Itoa(cfg.defaultItem) + "\n"
	content += "timeout " + strconv.Itoa(cfg.timeout) + "\n"
	content += "showmenu " + strconv.Itoa(cfg.showMenu) + "\n"
	for _, item := range cfg.items {
		content += fmt.Sprintf(template, item.title, item.kernel, item.initrd, item.args)
	}

	return ioutil.WriteFile(filename, []byte(content), 0644)
}
