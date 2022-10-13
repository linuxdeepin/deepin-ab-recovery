// SPDX-FileCopyrightText: 2022 UnionTech Software Technology Co., Ltd.
//
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"
)

type Config struct {
	Current string
	Backup  string
	Version string     `json:",omitempty"`
	Time    *time.Time `json:",omitempty"`
}

func loadConfig(filename string, c *Config) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	err = json.Unmarshal(content, c)
	if err != nil {
		return err
	}
	return nil
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
