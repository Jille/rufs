package config

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/sgielen/rufs/security"
	yaml "gopkg.in/yaml.v2"
)

var (
	configFlag = flag.String("config", "~/.rufs2", "Path to your RUFS2 config file (or folder)")

	configDir  string
	configFile string
	cfg        *Config
)

func MustResolvePath() {
	if err := resolvePath(); err != nil {
		log.Fatal(err)
	}
}

func resolvePath() error {
	if !flag.Parsed() {
		return fmt.Errorf("LoadConfig() called before flag.Parse()")
	}
	path := *configFlag
	if strings.HasPrefix(path, "~/") {
		usr, err := user.Current()
		if err != nil {
			return err
		}
		path = filepath.Join(usr.HomeDir, path[2:])
	}
	if strings.HasSuffix(path, ".yaml") {
		configFile = path
		configDir = filepath.Dir(path)
	} else {
		configDir = path
		configFile = filepath.Join(path, "config.yaml")
	}
	return nil
}

func LoadConfig() error {
	if err := resolvePath(); err != nil {
		return err
	}
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}
	cfg, err = parseConfig(data)
	if err != nil {
		return err
	}
	return nil
}

func LoadEmptyConfig() {
	cfg = &Config{}
}

func assertParsed() {
	if cfg == nil {
		panic("attempt to use config before calling config.LoadConfig()")
	}
}

func assertResolved() {
	if configDir == "" {
		panic("attempt to use config before calling config.LoadConfig() or config.MustLoadPath()")
	}
}

func PKIFiles(circle string) (ca, crt, key string) {
	assertResolved()
	return filepath.Join(configDir, "pki", circle, "ca.crt"), filepath.Join(configDir, "pki", circle, "user.crt"), filepath.Join(configDir, "pki", circle, "user.key")
}

func LoadCerts(circle string) (*security.KeyPair, error) {
	caf, crtf, keyf := PKIFiles(circle)
	ca, err := readFile(caf)
	if err != nil {
		return nil, err
	}
	crt, err := readFile(crtf)
	if err != nil {
		return nil, err
	}
	key, err := readFile(keyf)
	if err != nil {
		return nil, err
	}
	return security.LoadKeyPair(ca, crt, key)
}

func readFile(fn string) ([]byte, error) {
	d, err := ioutil.ReadFile(fn)
	if err != nil {
		return nil, fmt.Errorf("error when reading %q: %v", fn, err)
	}
	return d, nil
}

type Share struct {
	Local  string
	Remote string
}

type Circle struct {
	Name   string
	Shares []*Share
}

type Config struct {
	Circles []*Circle
}

func parseConfig(data []byte) (*Config, error) {
	config := &Config{}
	err := yaml.NewDecoder(bytes.NewReader(data)).Decode(config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func GetCircles() []*Circle {
	assertParsed()
	return cfg.Circles
}

func GetCircle(name string) (*Circle, bool) {
	assertParsed()
	for _, ci := range cfg.Circles {
		if ci.Name == name {
			return ci, true
		}
	}
	return nil, false
}
