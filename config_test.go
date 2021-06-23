package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	C "gopkg.in/check.v1"
)

type configSuite struct{}

func init() {
	C.Suite(&configSuite{})
}

func (*configSuite) TestSuiteConfig(c *C.C) {
	dir := c.MkDir()
	tmpfile, err := ioutil.TempFile(dir, "ab-recovery.json")
	c.Assert(err, C.Equals, nil)

	var cfg Config
	isFakeData := false
	err = loadConfig(configFile, &cfg)
	if err != nil {
		if os.IsNotExist(err) {
			c.Log("TestSuiteConfig ReadFile error:", err)
			data := []byte("{\"Current\":\"a6903bdb-fff8-4c29-a189-a943682fa8e4\",\"Backup\":\"c180eb18-96df-47b3-9570-033528d34c3f\",\"Version\":\"20\",\"Time\":\"2021-06-02T13:16:22.3229104+08:00\"}")
			err = json.Unmarshal(data, &cfg)
			c.Assert(err, C.Equals, nil)
			isFakeData = true
		} else {
			c.Assert(err, C.Equals, nil)
		}
	}

	err = cfg.save(tmpfile.Name())
	c.Assert(err, C.Equals, nil)

	if !isFakeData {
		err = cfg.check()
		c.Assert(err, C.Equals, nil)
	}
}
