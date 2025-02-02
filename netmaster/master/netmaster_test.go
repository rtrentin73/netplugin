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

package master

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/contiv/netplugin/core"
	"github.com/contiv/netplugin/netmaster/intent"
	"github.com/contiv/netplugin/resources"
	"github.com/contiv/netplugin/state"
	"github.com/contiv/netplugin/utils"
)

var fakeDriver *state.FakeStateDriver

func applyConfig(t *testing.T, cfgBytes []byte) {
	cfg := &intent.Config{}
	err := json.Unmarshal(cfgBytes, cfg)
	if err != nil {
		t.Fatalf("error '%s' parsing config '%s'\n", err, cfgBytes)
	}

	_, err = resources.NewStateResourceManager(fakeDriver)
	if err != nil {
		log.Fatalf("state store initialization failed. Error: %s", err)
	}
	defer func() { resources.ReleaseStateResourceManager() }()

	for _, tenant := range cfg.Tenants {
		err = CreateTenant(fakeDriver, &tenant)
		if err != nil {
			t.Fatalf("error '%s' creating tenant\n", err)
		}

		err = CreateNetworks(fakeDriver, &tenant)
		if err != nil {
			t.Fatalf("error '%s' creating networks\n", err)
		}

		err = CreateEndpoints(fakeDriver, &tenant)
		if err != nil {
			t.Fatalf("error '%s' creating endpoints\n", err)
		}
	}

	fakeDriver.DumpState()
}

func verifyKeys(t *testing.T, keys []string) {

	for _, key := range keys {
		found := false
		for stateKey := range fakeDriver.TestState {
			if found = strings.Contains(stateKey, key); found {
				break
			}
		}
		if !found {
			t.Fatalf("key '%s' was not populated in db", key)
		}
	}
}

func verifyKeysDoNotExist(t *testing.T, keys []string) {

	for _, key := range keys {
		found := false
		for stateKey := range fakeDriver.TestState {
			if found = strings.Contains(stateKey, key); found {
				t.Fatalf("key '%s' was populated in db", key)
			}
		}
	}
}

func initFakeStateDriver(t *testing.T) {
	config := &core.Config{V: &state.FakeStateDriverConfig{}}
	cfgBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshalling configuration failed. Error: %s", err)
	}

	d, err := utils.NewStateDriver("fakedriver", string(cfgBytes))
	if err != nil {
		t.Fatalf("failed to init statedriver. Error: %s", err)
	}

	fakeDriver = d.(*state.FakeStateDriver)
	testMode = true
}

func deinitFakeStateDriver() {
	utils.ReleaseStateDriver()
	testMode = false
}

func TestVlanConfig(t *testing.T) {
	cfgBytes := []byte(`{
    "Tenants" : [{
        "Name"                      : "tenant-one",
        "DefaultNetType"            : "vlan",
        "SubnetPool"                : "11.1.0.0/16",
        "AllocSubnetLen"            : 24,
        "Vlans"                     : "11-28",
        "Networks"  : [{
            "Name"                  : "orange",
            "Endpoints" : [{
                "Container"         : "myContainer1"
            },
            {
                "Container"         : "myContainer2"
            }]
        },
        {
            "Name"                  : "purple",
            "Endpoints" : [{
                "Container"         : "myContainer3"
            },
            {
                "Container"         : "myContainer4"
            }]
        }]
    }]}`)

	initFakeStateDriver(t)
	defer deinitFakeStateDriver()

	applyConfig(t, cfgBytes)

	keys := []string{"tenant-one", "orange", "myContainer1", "myContainer2",
		"purple", "myContainer3", "myContainer4"}

	verifyKeys(t, keys)
}

func TestVlanWithUnderlayConfig(t *testing.T) {
	cfgBytes := []byte(`{
    "Tenants" : [{
        "Name"                      : "tenant-one",
        "DefaultNetType"          : "vlan",
        "SubnetPool"              : "11.1.0.0/16",
        "AllocSubnetLen"          : 24,
        "Vlans"                   : "11-48",
        "Networks"  : [{
            "Name"                : "orange",
            "Endpoints" : [{
                "Container"       : "myContainer1",
                "Host"            : "host1"
            },
            {
                "Container"       : "myContainer3",
                "Host"            : "host2"
            }]
        },
        {
            "Name"                : "purple",
            "Endpoints" : [{
                "Container"       : "myContainer2",
                "Host"            : "host1"
            },
            {
                "Container"       : "myContainer4",
                "Host"            : "host2"
            }]
        }
        ]
    }]}`)

	initFakeStateDriver(t)
	defer deinitFakeStateDriver()

	applyConfig(t, cfgBytes)

	keys := []string{"tenant-one", "nets/orange", "nets/purple",
		"myContainer1", "myContainer2",
		"myContainer3", "myContainer4"}

	verifyKeys(t, keys)
}

func TestVxlanConfig(t *testing.T) {
	cfgBytes := []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant-one",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vxlans"                : "10001-14000",
        "Networks"  : [{
            "Name"              : "orange",
            "Endpoints" : [
            {
                "Container"     : "myContainer1",
                "Host"          : "host1"
            },
            {
                "Container"     : "myContainer3",
                "Host"          : "host2"
            }
            ]
        },
        {
            "Name"              : "purple",
            "Endpoints" : [{
                "Container"     : "myContainer2",
                "Host"          : "host1"
            },
            {
                "Container"     : "myContainer4",
                "Host"          : "host2"
            }]
        }]
    }]}`)

	initFakeStateDriver(t)
	defer deinitFakeStateDriver()

	applyConfig(t, cfgBytes)

	keys := []string{"tenant-one", "nets/orange", "nets/purple",
		"myContainer1", "myContainer2",
		"myContainer3", "myContainer4"}

	verifyKeys(t, keys)
}

func TestVxlanConfigWithLateHostBindings(t *testing.T) {
	cfgBytes := []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant-one",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vxlans"                : "10001-14000",
        "Networks"  : [{
            "Name"              : "orange",
            "Endpoints" : [
            {
                "Container"     : "myContainer1"
            },
            {
                "Container"     : "myContainer3"
            }
            ]
        },
        {
            "Name"              : "purple",
            "Endpoints" : [{
                "Container"     : "myContainer2"
            },
            {
                "Container"     : "myContainer4"
            }]
        }]
    }]}`)

	initFakeStateDriver(t)
	defer deinitFakeStateDriver()

	applyConfig(t, cfgBytes)
	fakeDriver.DumpState()

	keys := []string{"tenant-one", "nets/orange", "nets/purple"}

	verifyKeys(t, keys)

	epBindings := []intent.ConfigEP{{
		Container: "myContainer1",
		Host:      "host1",
	}, {
		Container: "myContainer2",
		Host:      "host1",
	}, {
		Container: "myContainer3",
		Host:      "host2",
	}, {
		Container: "myContainer4",
		Host:      "host2",
	}}

	err := CreateEpBindings(&epBindings)
	if err != nil {
		t.Fatalf("error '%s' creating tenant\n", err)
	}

	keys = []string{"tenant-one", "nets/orange", "nets/purple",
		"myContainer1", "myContainer2",
		"myContainer3", "myContainer4"}

	fakeDriver.DumpState()

	verifyKeys(t, keys)
}

// Tests for https://github.com/contiv/netplugin/issues/214
func TestConfigPktTagOutOfRange(t *testing.T) {
	CfgBytes := []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant1",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net1",
            "PktTag"            : 2000,
            "PktTagType"        : "vxlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, true)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant2",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net2",
            "PktTag"            : 2001,
            "PktTagType"        : "vxlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, false)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant3",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net3",
            "PktTag"            : 3000,
            "PktTagType"        : "vxlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, false)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant4",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net4",
            "PktTag"            : 3001,
            "PktTagType"        : "vxlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, true)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant5",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vlans"                 : "1201-1500",
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net5",
            "PktTag"            : 1200,
            "PktTagType"        : "vlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, true)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant6",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vlans"                 : "1201-1500",
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net6",
            "PktTag"            : 1201,
            "PktTagType"        : "vlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, false)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant7",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vlans"                 : "1201-1500",
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net7",
            "PktTag"            : 1500,
            "PktTagType"        : "vlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, false)

	CfgBytes = []byte(`{
    "Tenants" : [{
        "Name"                  : "tenant8",
        "DefaultNetType"        : "vxlan",
        "SubnetPool"            : "11.1.0.0/16",
        "AllocSubnetLen"        : 24,
        "Vlans"                 : "1201-1500",
        "Vxlans"                : "2001-3000",
        "Networks"  : [{
            "Name"              : "net8",
            "PktTag"            : 1501,
            "PktTagType"        : "vlan"
        }]
    }]}`)
	applyVerifyRangeTag(t, CfgBytes, true)

}

func applyVerifyRangeTag(t *testing.T, cfgBytes []byte, shouldFail bool) {
	initFakeStateDriver(t)
	defer deinitFakeStateDriver()

	cfg := &intent.Config{}
	err := json.Unmarshal(cfgBytes, cfg)
	if err != nil {
		t.Fatalf("error '%s' parsing config '%s'\n", err, cfgBytes)
	}

	_, err = resources.NewStateResourceManager(fakeDriver)
	if err != nil {
		log.Fatalf("state store initialization failed. Error: %s", err)
	}
	defer func() { resources.ReleaseStateResourceManager() }()

	tenant := cfg.Tenants[0]
	err = CreateTenant(fakeDriver, &tenant)
	if err != nil {
		t.Fatalf("error '%s' creating tenant\n", err)
	}

	err = CreateNetworks(fakeDriver, &tenant)
	if shouldFail {

		var expError string
		network := tenant.Networks[0]
		if network.PktTagType == "vlan" {
			expError = fmt.Sprintf("vlan %d does not adhere to tenant's vlan range %s", network.PktTag, tenant.VLANs)
		} else {
			expError = fmt.Sprintf("vxlan %d does not adhere to tenant's vxlan range %s", network.PktTag, tenant.VXLANs)
		}

		if err == nil {
			t.Fatalf("CreateNetworks did not return error\n")
		} else if err.Error() != expError {
			t.Fatalf("CreateNetworks did not return error for OutOfRange\n")
		}
	} else if err != nil {
		t.Fatalf("error '%s' while creating network\n", err)
	}

}
