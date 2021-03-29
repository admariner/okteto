// Copyright 2020 The Okteto Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ssh

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/okteto/okteto/pkg/config"
)

func buildHostname(name string) string {
	return fmt.Sprintf("%s.okteto", name)
}

// AddEntry adds an entry to the user's sshconfig
func AddEntry(name, iface string, port int) error {
	return add(getSSHConfigPath(), buildHostname(name), iface, port)
}

func add(path, name, iface string, port int) error {
	cfg, err := getConfig(path)
	if err != nil {
		return err
	}

	_ = removeHost(cfg, name)

	_, privateKey := getKeyPaths()

	host := newHost([]string{name}, []string{"entry generated by okteto"})
	host.params = []*param{
		newParam(forwardAgentKeyword, []string{"yes"}, nil),
		newParam(hostNameKeyword, []string{iface}, nil),
		newParam(portKeyword, []string{strconv.Itoa(port)}, nil),
		newParam(strictHostKeyCheckingKeyword, []string{"no"}, nil),
		newParam(userKnownHostsFileKeyword, []string{"/dev/null"}, nil),
		newParam(identityFile, []string{"\"" + privateKey + "\""}, nil),
	}

	cfg.hosts = append(cfg.hosts, host)
	return save(cfg, path)
}

// RemoveEntry removes the entry to the user's sshconfig if found
func RemoveEntry(name string) error {
	return remove(getSSHConfigPath(), buildHostname(name))
}

// GetPort returns the corresponding SSH port for the dev env
func GetPort(name string) (int, error) {
	cfg, err := getConfig(getSSHConfigPath())
	if err != nil {
		return 0, err
	}

	hostname := buildHostname(name)
	i, found := findHost(cfg, hostname)
	if !found {
		return 0, fmt.Errorf("development container not found")
	}

	param := cfg.hosts[i].getParam(portKeyword)
	if param == nil {
		return 0, fmt.Errorf("port not found")
	}

	port, err := strconv.Atoi(param.value())
	if err != nil {
		return 0, fmt.Errorf("invalid port: %s", param.value())
	}

	return port, nil
}

func remove(path, name string) error {
	cfg, err := getConfig(path)
	if err != nil {
		return err
	}

	if removeHost(cfg, name) {
		return save(cfg, path)
	}

	return nil
}

func removeHost(cfg *sshConfig, name string) bool {
	ix, ok := findHost(cfg, name)
	if ok {
		cfg.hosts = append(cfg.hosts[:ix], cfg.hosts[ix+1:]...)
		return true
	}

	return false
}

func findHost(cfg *sshConfig, name string) (int, bool) {
	for i, h := range cfg.hosts {
		for _, hn := range h.hostnames {
			if hn == name {
				p := h.getParam(portKeyword)
				s := h.getParam(strictHostKeyCheckingKeyword)
				if p != nil && s != nil {
					return i, true
				}
			}
		}
	}

	return 0, false
}

func getConfig(path string) (*sshConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &sshConfig{
				hosts: []*host{},
			}, nil
		}

		return nil, fmt.Errorf("can't open %s: %s", path, err)
	}

	defer f.Close()

	cfg, err := parse(f)
	if err != nil {
		return nil, fmt.Errorf("fail to decode %s: %s", path, err)
	}

	return cfg, nil
}

func save(cfg *sshConfig, path string) error {
	if err := cfg.writeToFilepath(path); err != nil {
		return fmt.Errorf("fail to update SSH config file %s: %w", path, err)
	}

	return nil
}

func getSSHConfigPath() string {
	return filepath.Join(config.GetUserHomeDir(), ".ssh", "config")
}
