package main

import (
	"fmt"
	"encoding/json"
	"io/ioutil"
)

type Config struct {
	Current string
	Backup  string
}

func loadConfig(filename string, c *Config)  error {
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

