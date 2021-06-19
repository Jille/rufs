package config

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sgielen/rufs/security"
	yaml "gopkg.in/yaml.v2"
)

var (
	configFlag = flag.String("config", "~/.rufs2", "Path to your RUFS2 config file (or folder)")

	configDir  string
	configFile string
	mtx        sync.Mutex
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
		d, err := homedir()
		if err != nil {
			return err
		}
		path = filepath.Join(d, path[2:])
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
	mtx.Lock()
	cfg, err = parseConfig(data)
	mtx.Unlock()
	if err != nil {
		return err
	}
	SetMountpoint(cfg.Mountpoint)
	return nil
}

func LoadEmptyConfig() {
	mtx.Lock()
	cfg = &Config{}
	mtx.Unlock()
}

func assertParsed() {
	mtx.Lock()
	defer mtx.Unlock()
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

func LoadAllCerts() (map[string]*security.KeyPair, error) {
	keyPairs := map[string]*security.KeyPair{}
	for _, c := range GetCircles() {
		kp, err := LoadCerts(c.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to read certificates: %v", err)
		}
		keyPairs[c.Name] = kp
	}
	return keyPairs, nil
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
	Shares []Share
}

type Config struct {
	Circles    []Circle
	Mountpoint string
}

func parseConfig(data []byte) (*Config, error) {
	config := &Config{}
	err := yaml.NewDecoder(bytes.NewReader(data)).Decode(config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func GetConfig() Config {
	assertParsed()
	mtx.Lock()
	defer mtx.Unlock()
	return *cfg
}

func GetCircles() []Circle {
	return GetConfig().Circles
}

func GetCircle(name string) (Circle, bool) {
	for _, ci := range GetConfig().Circles {
		if ci.Name == name {
			return ci, true
		}
	}
	return Circle{}, false
}

func currentOrDefaultConfig() (Config, error) {
	c := *cfg
	if c.Mountpoint == "" {
		var err error
		c.Mountpoint, err = defaultMountpoint()
		if err != nil {
			return c, err
		}
	}
	return c, nil
}

func AddCircleAndStore(name string) error {
	assertParsed()
	mtx.Lock()
	defer mtx.Unlock()
	newCfg, err := currentOrDefaultConfig()
	if err != nil {
		return err
	}
	newCfg.Circles = append(newCfg.Circles, Circle{
		Name: name,
	})
	return writeNewConfig(newCfg)
}

func AddShareAndStore(circle, share, local string) error {
	assertParsed()
	mtx.Lock()
	defer mtx.Unlock()
	newCfg, err := currentOrDefaultConfig()
	if err != nil {
		return err
	}
	for i, c := range newCfg.Circles {
		if c.Name == circle {
			newCfg.Circles[i].Shares = append(newCfg.Circles[i].Shares, Share{
				Remote: share,
				Local:  local,
			})
			return writeNewConfig(newCfg)
		}
	}
	return fmt.Errorf("unknown circle %q", circle)
}

func SetMountpointAndStore(mp string) error {
	assertParsed()
	mtx.Lock()
	defer mtx.Unlock()
	newCfg, err := currentOrDefaultConfig()
	if err != nil {
		return err
	}
	newCfg.Mountpoint = mp
	return writeNewConfig(newCfg)
}

func writeNewConfig(c Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(configFile, b, 0644); err != nil {
		return err
	}
	cfg = &c
	return nil
}
