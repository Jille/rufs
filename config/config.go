package config

import (
	"bytes"
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

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

func ReadConfigFile(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return ReadConfig(data)
}

func ReadConfig(data []byte) (*Config, error) {
	config := &Config{}
	err := yaml.NewDecoder(bytes.NewReader(data)).Decode(config)
	if err != nil {
		return nil, err
	}
	return config, nil
}
