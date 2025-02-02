/***
Copyright 2014 Cisco Systems Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"sync"

	"github.com/contiv/netplugin/core"
	"github.com/contiv/netplugin/utils"
)

// implements the generic Plugin interface

// Config has the configuration for the plugin
type Config struct {
	Drivers struct {
		Network  string `json:"network"`
		Endpoint string `json:"endpoint"`
		State    string `json:"state"`
	}
	Instance core.InstanceInfo `json:"plugin-instance"`
}

// NetPlugin is the configuration struct for the plugin bus. Network and
// Endpoint drivers are all present in `drivers/` and state drivers are present
// in `state/`.
type NetPlugin struct {
	sync.Mutex
	ConfigFile    string
	NetworkDriver core.NetworkDriver
	StateDriver   core.StateDriver
}

// Init initializes the NetPlugin instance via the configuration string passed.
func (p *NetPlugin) Init(pluginConfig Config, configStr string) error {
	var err error
	if pluginConfig.Instance.HostLabel == "" {
		return core.Errorf("empty host-label passed")
	}

	// initialize state driver
	p.StateDriver, err = utils.NewStateDriver(pluginConfig.Drivers.State, configStr)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			utils.ReleaseStateDriver()
		}
	}()

	instanceInfo := &core.InstanceInfo{
		HostLabel:   pluginConfig.Instance.HostLabel,
		VtepIP:      pluginConfig.Instance.VtepIP,
		VlanIntf:    pluginConfig.Instance.VlanIntf,
		StateDriver: p.StateDriver,
	}

	// initialize network driver
	p.NetworkDriver, err = utils.NewNetworkDriver(pluginConfig.Drivers.Network,
		configStr, instanceInfo)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			p.NetworkDriver.Deinit()
		}
	}()

	return nil
}

// Deinit is a destructor for the NetPlugin configuration.
func (p *NetPlugin) Deinit() {
	if p.NetworkDriver != nil {
		p.NetworkDriver.Deinit()
		p.NetworkDriver = nil
	}
	if p.StateDriver != nil {
		utils.ReleaseStateDriver()
		p.StateDriver = nil
	}
}

// CreateNetwork creates a network for a given ID.
func (p *NetPlugin) CreateNetwork(id string) error {
	return p.NetworkDriver.CreateNetwork(id)
}

// DeleteNetwork deletes a network provided by the ID.
func (p *NetPlugin) DeleteNetwork(id, encap string, pktTag, extPktTag int) error {
	return p.NetworkDriver.DeleteNetwork(id, encap, pktTag, extPktTag)
}

// FetchNetwork retrieves a network's state given an ID.
func (p *NetPlugin) FetchNetwork(id string) (core.State, error) {
	return nil, core.Errorf("Not implemented")
}

// CreateEndpoint creates an endpoint for a given ID.
func (p *NetPlugin) CreateEndpoint(id string) error {
	return p.NetworkDriver.CreateEndpoint(id)
}

// DeleteEndpoint destroys an endpoint for an ID.
func (p *NetPlugin) DeleteEndpoint(id string) error {
	return p.NetworkDriver.DeleteEndpoint(id)
}

// FetchEndpoint retrieves an endpoint's state for a given ID
func (p *NetPlugin) FetchEndpoint(id string) (core.State, error) {
	return nil, core.Errorf("Not implemented")
}

// AddPeerHost adds an peer host.
func (p *NetPlugin) AddPeerHost(node core.ServiceInfo) error {
	return p.NetworkDriver.AddPeerHost(node)
}

// DeletePeerHost removes a peer host.
func (p *NetPlugin) DeletePeerHost(node core.ServiceInfo) error {
	return p.NetworkDriver.DeletePeerHost(node)
}

// AddMaster adds a master node.
func (p *NetPlugin) AddMaster(node core.ServiceInfo) error {
	return p.NetworkDriver.AddMaster(node)
}

// DeleteMaster removes a master node
func (p *NetPlugin) DeleteMaster(node core.ServiceInfo) error {
	return p.NetworkDriver.DeleteMaster(node)
}
