package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	testDataPath := "./TemporaryTestDataDirectoryNeedDelete"
	err := os.Mkdir(testDataPath, 0777)
	require.NoError(t, err)
	defer func() {
		err := os.RemoveAll(testDataPath)
		require.NoError(t, err)
	}()
	tmpfile, err := ioutil.TempFile(testDataPath, "ab-recovery.json")
	require.NoError(t, err)
	defer tmpfile.Close()

	var cfg Config
	data := []byte(`{"Current":"a6903bdb-fff8-4c29-a189-a943682fa8e4","Backup":"c180eb18-96df-47b3-9570-033528d34c3f","Version":"20","Time":"2021-06-02T13:16:22.3229104+08:00"}`)
	err = json.Unmarshal(data, &cfg)
	require.NoError(t, err)
	err = cfg.save(tmpfile.Name())
	require.NoError(t, err)

	var cfgLoad Config
	err = loadConfig(tmpfile.Name(), &cfgLoad)
	require.NoError(t, err)
	assert.Equal(t, cfg.Current, cfgLoad.Current)
	assert.Equal(t, cfg.Backup, cfgLoad.Backup)
	assert.Equal(t, cfg.Version, cfgLoad.Version)
	assert.Equal(t, cfg.Time, cfgLoad.Time)
}

func TestConfigCheck(t *testing.T) {
	var cfg Config
	err := loadConfig(configFile, &cfg)
	if err != nil {
		t.Skip("file not exist")
	}
	err = cfg.check()
	require.NoError(t, err)
}
