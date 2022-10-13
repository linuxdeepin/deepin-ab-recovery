// SPDX-FileCopyrightText: 2018 - 2022 UnionTech Software Technology Co., Ltd.
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func getHideWhat(str string) string {
	fields := strings.SplitN(str, "hide", 2)
	if len(fields) == 2 && strings.Contains(fields[0], "#") {
		return strings.TrimSpace(fields[1])
	}
	return ""
}

var _regUuidIgn = regexp.MustCompile(`ENV{ID_FS_UUID}=="([^"]+)".*ENV{UDISKS_IGNORE}="1"`)

func matchUuidIgnore(str string) bool {
	return _regUuidIgn.MatchString(str)
}

func getIgnoredUuid(str string) string {
	match := _regUuidIgn.FindStringSubmatch(str)
	if match != nil {
		return match[1]
	}
	return ""
}

func replaceUuid(uuid string) string {
	return fmt.Sprintf(`ENV{ID_FS_UUID}=="%s", ENV{UDISKS_IGNORE}="1"`, uuid)
}

// 参数 uuid： 新的 uuid。
// 参数 otherUuid： 用来替换的目标，在备份流程中，这个 uuid 是备份分区 uuid。
// 参数 newHideWhat: 对应 uuid 所指分区的 label。
func modifyRulesFunc(lines []string, labelUuidMap map[string]string, uuid, otherUuid, newHideWhat string) []string {
	// 修正因为执行本程序的 bug，导致了错误的第二行，第二行可能是为了隐藏 efi 或 boot 等分区，
	// 而非是为了隐藏备份分区。
	const abRecBackupPart = "ab-recovery backup partition"
	if len(lines) >= 2 {
		hideWhat := getHideWhat(lines[0])
		if hideWhat != "" && hideWhat != abRecBackupPart && matchUuidIgnore(lines[1]) {
			newUuid := ""
			for label, uuid := range labelUuidMap {
				if strings.EqualFold(hideWhat, label) {
					newUuid = uuid
					break
				}
			}

			if newUuid != "" {
				lines[1] = replaceUuid(newUuid)
			}
		}
	}

	// 去掉空行，空行可能产生干扰
	tempLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		tempLines = append(tempLines, line)
	}
	lines = tempLines

	newHideWhat = strings.ToLower(newHideWhat)
	replaceDone := false
	for i := 0; i < len(lines)-1; i++ {
		thisLine := &lines[i]
		nextLine := &lines[i+1]
		if getIgnoredUuid(*nextLine) == otherUuid {
			*thisLine = "# hide " + newHideWhat
			*nextLine = replaceUuid(uuid)
			replaceDone = true
			break
		}
	}

	if !replaceDone {
		// 可能出现不能根据 otherUuid 找到要替换的记录，那么就是 uuid 错误的情况，根据注释中的信息再次尝试寻找。
		for i := 0; i < len(lines)-1; i++ {
			thisLine := &lines[i]
			nextLine := &lines[i+1]
			hideWhat := getHideWhat(*thisLine)
			if strings.EqualFold(hideWhat, "roota") || strings.EqualFold(hideWhat, "rootb") {
				*thisLine = "# hide " + newHideWhat
				*nextLine = replaceUuid(uuid)
			}
		}
	}

	return lines
}

// 系统备份升级后还原，由于备份分区未被隐藏，导致文件管理器显示的系统盘不是真正的系统盘
// 显示的是备份分区的系统盘,所以在备份时,修改备份分区下的rule文件,将备份分区继续继续隐藏。
func modifyRules(filename string, labelUuidMap map[string]string, uuid, otherUuid, newHideWhat string) error {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	lines = modifyRulesFunc(lines, labelUuidMap, uuid, otherUuid, newHideWhat)
	var buf bytes.Buffer
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	data = buf.Bytes()
	err = ioutil.WriteFile(filename+".new", data, 0644)
	if err != nil {
		return err
	}
	err = os.Rename(filename+".new", filename)
	if err != nil {
		return err
	}
	return nil
}

func reloadUdev() error {
	err := exec.Command("udevadm", "control", "--reload-rules").Run()
	if err != nil {
		logger.Warning(err)
		return err
	}
	err = exec.Command("udevadm", "trigger").Run()
	if err != nil {
		logger.Warning(err)
		return err
	}
	return nil
}

