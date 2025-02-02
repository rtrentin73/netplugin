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
package ofnet

// This file implements ofnet agent API which runs on each host alongside OVS.
// This assumes:
//      - ofnet agent is running on each host
//      - There is single OVS switch instance(aka bridge instance)
//      - OVS switch's forwarding is fully controller by ofnet agent
//
// It also assumes OVS is configured for openflow1.3 version and configured
// to connect to controller on specified port

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"time"

	"github.com/contiv/ofnet/ofctrl"
	"github.com/contiv/ofnet/rpcHub"

	log "github.com/Sirupsen/logrus"
)

// OfnetAgent state
type OfnetAgent struct {
	ctrler      *ofctrl.Controller // Controller instance
	ofSwitch    *ofctrl.OFSwitch   // Switch instance. Assumes single switch per agent
	localIp     net.IP             // Local IP to be used for tunnel end points
	MyPort      uint16             // Port where the agent's RPC server is listening
	MyAddr      string             // RPC server addr. same as localIp. different in testing environments
	isConnected bool               // Is the switch connected

	rpcServ     *rpc.Server   // jsonrpc server
	rpcListener net.Listener  // Listener
	datapath    OfnetDatapath // Configured datapath

	masterDb map[string]*OfnetNode // list of Masters

	// Port and VNI to vlan mapping table
	portVlanMap map[uint32]*uint16 // Map port number to vlan
	vniVlanMap  map[uint32]*uint16 // Map VNI to vlan
	vlanVniMap  map[uint16]*uint32 // Map vlan to VNI

	// VTEP database
	vtepTable map[string]*uint32 // Map vtep IP to OVS port number

	// Endpoint database
	endpointDb      map[string]*OfnetEndpoint // all known endpoints
	localEndpointDb map[uint32]*OfnetEndpoint // local port to endpoint map
}

// local End point information
type EndpointInfo struct {
	PortNo        uint32
	EndpointGroup int
	MacAddr       net.HardwareAddr
	Vlan          uint16
	IpAddr        net.IP
}

const FLOW_MATCH_PRIORITY = 100        // Priority for all match flows
const FLOW_FLOOD_PRIORITY = 10         // Priority for flood entries
const FLOW_MISS_PRIORITY = 1           // priority for table miss flow
const FLOW_POLICY_PRIORITY_OFFSET = 10 // Priority offset for policy rules

const VLAN_TBL_ID = 1
const DST_GRP_TBL_ID = 2
const POLICY_TBL_ID = 3
const IP_TBL_ID = 4
const MAC_DEST_TBL_ID = 5

// Create a new Ofnet agent and initialize it
func NewOfnetAgent(dpName string, localIp net.IP, rpcPort uint16, ovsPort uint16) (*OfnetAgent, error) {
	agent := new(OfnetAgent)

	// Init params
	agent.localIp = localIp
	agent.MyPort = rpcPort
	agent.MyAddr = localIp.String()

	agent.masterDb = make(map[string]*OfnetNode)
	agent.portVlanMap = make(map[uint32]*uint16)
	agent.vniVlanMap = make(map[uint32]*uint16)
	agent.vlanVniMap = make(map[uint16]*uint32)

	// Initialize vtep database
	agent.vtepTable = make(map[string]*uint32)

	// Initialize endpoint database
	agent.endpointDb = make(map[string]*OfnetEndpoint)
	agent.localEndpointDb = make(map[uint32]*OfnetEndpoint)

	// Create an openflow controller
	agent.ctrler = ofctrl.NewController(agent)

	// Start listening to controller port
	go agent.ctrler.Listen(fmt.Sprintf(":%d", ovsPort))

	// Create rpc server
	// FIXME: Figure out how to handle multiple OVS bridges.
	rpcServ, listener := rpcHub.NewRpcServer(rpcPort)
	agent.rpcServ = rpcServ
	agent.rpcListener = listener

	// Register for Master add/remove events
	rpcServ.Register(agent)

	// Create the datapath
	switch dpName {
	case "vrouter":
		agent.datapath = NewVrouter(agent, rpcServ)
	case "vxlan":
		agent.datapath = NewVxlan(agent, rpcServ)
	case "vlan":
		agent.datapath = NewVlanBridge(agent, rpcServ)
	default:
		log.Fatalf("Unknown Datapath %s", dpName)
	}

	// Return it
	return agent, nil
}

// getEndpointId Get a unique identifier for the endpoint.
// FIXME: This needs to be VRF, IP address.
func (self *OfnetAgent) getEndpointId(endpoint EndpointInfo) string {
	return endpoint.IpAddr.String()
}

func (self *OfnetAgent) getEndpointByIp(ipAddr net.IP) *OfnetEndpoint {
	return self.endpointDb[ipAddr.String()]
}

// Delete cleans up an ofnet agent
func (self *OfnetAgent) Delete() error {
	// Disconnect from the switch
	if self.ofSwitch != nil {
		self.ofSwitch.Disconnect()
	}

	// Cleanup the controller
	self.ctrler.Delete()

	// close listeners
	self.rpcListener.Close()

	time.Sleep(100 * time.Millisecond)

	return nil
}

// Handle switch connected event
func (self *OfnetAgent) SwitchConnected(sw *ofctrl.OFSwitch) {
	log.Infof("Switch %v connected", sw.DPID())

	// store it for future use.
	self.ofSwitch = sw

	// Inform the datapath
	self.datapath.SwitchConnected(sw)

	self.isConnected = true
}

// Handle switch disconnect event
func (self *OfnetAgent) SwitchDisconnected(sw *ofctrl.OFSwitch) {
	log.Infof("Switch %v disconnected", sw.DPID())

	// Inform the datapath
	self.datapath.SwitchDisconnected(sw)

	self.ofSwitch = nil
	self.isConnected = false
}

// IsSwitchConnected returns true if switch is connected
func (self *OfnetAgent) IsSwitchConnected() bool {
	return self.isConnected
}

// WaitForSwitchConnection wait till switch connects
func (self *OfnetAgent) WaitForSwitchConnection() {
	// Wait for a while for OVS switch to connect to ofnet agent
	for i := 0; i < 20; i++ {
		time.Sleep(1 * time.Second)
		if self.IsSwitchConnected() {
			break
		}
	}
}

// Receive a packet from the switch.
func (self *OfnetAgent) PacketRcvd(sw *ofctrl.OFSwitch, pkt *ofctrl.PacketIn) {
	log.Infof("Packet received from switch %v. Packet: %+v", sw.DPID(), pkt)
	log.Infof("Input Port: %+v", pkt.Match.Fields[0].Value)

	// Inform the datapath
	self.datapath.PacketRcvd(sw, pkt)
}

// Add a master
// ofnet agent tries to connect to the master and download routes
func (self *OfnetAgent) AddMaster(masterInfo *OfnetNode, ret *bool) error {
	master := new(OfnetNode)
	master.HostAddr = masterInfo.HostAddr
	master.HostPort = masterInfo.HostPort

	var resp bool

	log.Infof("Adding master: %+v", *master)

	masterKey := fmt.Sprintf("%s:%d", masterInfo.HostAddr, masterInfo.HostPort)

	// Save it in DB
	self.masterDb[masterKey] = master

	// My info to send to master
	myInfo := new(OfnetNode)
	myInfo.HostAddr = self.MyAddr
	myInfo.HostPort = self.MyPort

	// Register the agent with the master
	err := rpcHub.Client(master.HostAddr, master.HostPort).Call("OfnetMaster.RegisterNode", &myInfo, &resp)
	if err != nil {
		log.Errorf("Failed to register with the master %+v. Err: %v", master, err)
		return err
	}

	// Perform master added callback so that datapaths can send their FDB to master
	err = self.datapath.MasterAdded(master)
	if err != nil {
		log.Errorf("Error making master added callback for %+v. Err: %v", master, err)
	}

	// Send all local endpoints to new master.
	for _, endpoint := range self.localEndpointDb {
		if endpoint.OriginatorIp.String() == self.localIp.String() {
			var resp bool

			log.Infof("Sending endpoint %+v to master %+v", endpoint, master)

			// Make the RPC call to add the endpoint to master
			client := rpcHub.Client(master.HostAddr, master.HostPort)
			err := client.Call("OfnetMaster.EndpointAdd", endpoint, &resp)
			if err != nil {
				log.Errorf("Failed to add endpoint %+v to master %+v. Err: %v", endpoint, master, err)
				return err
			}
		}
	}

	return nil
}

// Remove the master from master DB
func (self *OfnetAgent) RemoveMaster(masterInfo *OfnetNode) error {
	log.Infof("Deleting master: %+v", masterInfo)

	masterKey := fmt.Sprintf("%s:%d", masterInfo.HostAddr, masterInfo.HostPort)

	// Remove it from DB
	delete(self.masterDb, masterKey)

	return nil
}

// Add a local endpoint.
// This takes ofp port number, mac address, vlan and IP address of the port.
func (self *OfnetAgent) AddLocalEndpoint(endpoint EndpointInfo) error {
	// Add port vlan mapping
	self.portVlanMap[endpoint.PortNo] = &endpoint.Vlan

	// Map Vlan to VNI
	vni := self.vlanVniMap[endpoint.Vlan]
	if vni == nil {
		log.Errorf("VNI for vlan %d is not known", endpoint.Vlan)
		return errors.New("Unknown Vlan")
	}

	epId := self.getEndpointId(endpoint)

	// Build endpoint registry info
	epreg := &OfnetEndpoint{
		EndpointID:    epId,
		EndpointType:  "internal",
		EndpointGroup: endpoint.EndpointGroup,
		IpAddr:        endpoint.IpAddr,
		VrfId:         0, // FIXME set VRF correctly
		MacAddrStr:    endpoint.MacAddr.String(),
		Vlan:          endpoint.Vlan,
		Vni:           *vni,
		OriginatorIp:  self.localIp,
		PortNo:        endpoint.PortNo,
		Timestamp:     time.Now(),
	}

	// Call the datapath
	err := self.datapath.AddLocalEndpoint(*epreg)
	if err != nil {
		log.Errorf("Adding endpoint (%+v) to datapath. Err: %v", epreg, err)
		return err
	}

	// Add the endpoint to local routing table
	self.endpointDb[epId] = epreg
	self.localEndpointDb[endpoint.PortNo] = epreg

	// Send the endpoint to all known masters
	for _, master := range self.masterDb {
		var resp bool

		log.Infof("Sending endpoint %+v to master %+v", epreg, master)

		// Make the RPC call to add the endpoint to master
		err := rpcHub.Client(master.HostAddr, master.HostPort).Call("OfnetMaster.EndpointAdd", epreg, &resp)
		if err != nil {
			log.Errorf("Failed to add endpoint %+v to master %+v. Err: %v", epreg, master, err)
			return err
		}
	}

	return nil
}

// Remove local endpoint
func (self *OfnetAgent) RemoveLocalEndpoint(portNo uint32) error {
	// Clear it from DB
	delete(self.portVlanMap, portNo)

	epreg := self.localEndpointDb[portNo]
	if epreg == nil {
		log.Errorf("Endpoint not found for port %d", portNo)
		return errors.New("Endpoint not found")
	}

	// Call the datapath
	err := self.datapath.RemoveLocalEndpoint(*epreg)
	if err != nil {
		log.Errorf("Error deleting endpointon port %d. Err: %v", portNo, err)
	}

	// delete the endpoint from local endpoint table
	delete(self.endpointDb, epreg.EndpointID)
	delete(self.localEndpointDb, portNo)

	// Send the DELETE to all known masters
	for _, master := range self.masterDb {
		var resp bool

		log.Infof("Sending DELETE endpoint %+v to master %+v", epreg, master)

		// Make the RPC call to delete the endpoint on master
		client := rpcHub.Client(master.HostAddr, master.HostPort)
		err := client.Call("OfnetMaster.EndpointDel", epreg, &resp)
		if err != nil {
			log.Errorf("Failed to DELETE endpoint %+v on master %+v. Err: %v", epreg, master, err)
		}
	}

	return nil
}

// Add virtual tunnel end point. This is mainly used for mapping remote vtep IP
// to ofp port number.
func (self *OfnetAgent) AddVtepPort(portNo uint32, remoteIp net.IP) error {
	// Ignore duplicate Add vtep messages
	oldPort, ok := self.vtepTable[remoteIp.String()]
	if ok && *oldPort == portNo {
		return nil
	}

	log.Infof("Adding VTEP port(%d), Remote IP: %v", portNo, remoteIp)

	// Store the vtep IP to port number mapping
	self.vtepTable[remoteIp.String()] = &portNo

	// Call the datapath
	return self.datapath.AddVtepPort(portNo, remoteIp)
}

// Remove a VTEP port
func (self *OfnetAgent) RemoveVtepPort(portNo uint32, remoteIp net.IP) error {
	// Clear the vtep IP to port number mapping
	delete(self.vtepTable, remoteIp.String())

	// walk all the endpoints and uninstall the ones pointing at remote host
	for _, endpoint := range self.endpointDb {
		// Find all the routes pointing at the remote VTEP
		if endpoint.OriginatorIp.String() == remoteIp.String() {
			var resp bool
			// Uninstall the route from HW
			err := self.EndpointDel(endpoint, &resp)
			if err != nil {
				log.Errorf("Error uninstalling endpoint %+v. Err: %v", endpoint, err)
			}
		}
	}

	// Call the datapath
	return self.datapath.RemoveVtepPort(portNo, remoteIp)
}

// Add a vlan.
// This is mainly used for mapping vlan id to Vxlan VNI
func (self *OfnetAgent) AddVlan(vlanId uint16, vni uint32) error {
	// store it in DB
	self.vlanVniMap[vlanId] = &vni
	self.vniVlanMap[vni] = &vlanId

	// Call the datapath
	return self.datapath.AddVlan(vlanId, vni)
}

// Remove a vlan from datapath
func (self *OfnetAgent) RemoveVlan(vlanId uint16, vni uint32) error {
	// Clear the database
	delete(self.vlanVniMap, vlanId)
	delete(self.vniVlanMap, vni)

	// make sure there are no endpoints still installed in this vlan
	for _, endpoint := range self.endpointDb {
		if endpoint.Vni == vni {
			log.Fatalf("Vlan %d still has routes. Route: %+v", vlanId, endpoint)
		}
	}

	// Call the datapath
	return self.datapath.RemoveVlan(vlanId, vni)
}

// Add remote endpoint RPC call from master
func (self *OfnetAgent) EndpointAdd(epreg *OfnetEndpoint, ret *bool) error {
	log.Infof("EndpointAdd rpc call for endpoint: %+v. localIp: %v", epreg, self.localIp)

	// If this is a local endpoint we are done
	if epreg.OriginatorIp.String() == self.localIp.String() {
		return nil
	}

	// Check if we have the endpoint already and which is more recent
	oldEp := self.endpointDb[epreg.EndpointID]
	if oldEp != nil {
		// If old endpoint has more recent timestamp, nothing to do
		if !epreg.Timestamp.After(oldEp.Timestamp) {
			return nil
		}

		// Uninstall the old endpoint from datapath
		err := self.datapath.RemoveEndpoint(oldEp)
		if err != nil {
			log.Errorf("Error deleting old endpoint: {%+v}. Err: %v", oldEp, err)
		}
	}

	// First, add the endpoint to local routing table
	self.endpointDb[epreg.EndpointID] = epreg

	// Lookup the VTEP for the endpoint
	vtepPort := self.vtepTable[epreg.OriginatorIp.String()]
	if vtepPort == nil {
		log.Errorf("Could not find the VTEP for endpoint: %+v", epreg)

		return errors.New("VTEP not found")
	}

	// Install the endpoint in datapath
	err := self.datapath.AddEndpoint(epreg)
	if err != nil {
		log.Errorf("Error adding endpoint: {%+v}. Err: %v", epreg, err)
		return err
	}

	return nil
}

// Delete remote endpoint RPC call from master
func (self *OfnetAgent) EndpointDel(epreg *OfnetEndpoint, ret *bool) error {
	// If this is a local endpoint we are done
	if epreg.OriginatorIp.String() == self.localIp.String() {
		return nil
	}

	// Ignore duplicate delete requests we might receive from multiple
	// Ofnet masters
	if self.endpointDb[epreg.EndpointID] == nil {
		return nil
	}

	// Uninstall the endpoint from datapath
	err := self.datapath.RemoveEndpoint(epreg)
	if err != nil {
		log.Errorf("Error deleting endpoint: {%+v}. Err: %v", epreg, err)
	}

	// Remove it from endpoint table
	delete(self.endpointDb, epreg.EndpointID)

	return nil
}

func (self *OfnetAgent) DummyRpc(arg *string, ret *bool) error {
	log.Infof("Received dummy route RPC call")
	return nil
}
