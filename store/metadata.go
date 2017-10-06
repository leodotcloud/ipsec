package store

import (
	"fmt"
	"net"
	"strings"

	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
)

const (
	metadataURLTemplate = "http://%v/2015-12-19"
	defaultSubnetPrefix = "/16"

	// DefaultMetadataAddress specifies the default value to use if nothing is specified
	DefaultMetadataAddress = "169.254.169.250"
)

// MetadataStore contains information related to metadata client, etc
type MetadataStore struct {
	mc                metadata.Client
	self              Entry
	entries           []Entry
	local             map[string]Entry
	remote            map[string]Entry
	peersMap          map[string]Entry
	remoteNonPeersMap map[string]Entry
	info              *InfoFromMetadata
}

// InfoFromMetadata stores the information that has been fetched from
// metadata server
type InfoFromMetadata struct {
	selfHost                metadata.Host
	selfNetwork             metadata.Network
	selfNetworkSubnetPrefix string
	services                []metadata.Service
	servicesMapByName       map[string][]*metadata.Service
	hosts                   []metadata.Host
	containers              []metadata.Container
	hostsMap                map[string]metadata.Host
	networksMap             map[string]metadata.Network
}

// NewMetadataStoreWithClientIP creates, intializes and returns a store for use with a specific Client IP to contact the metadata
func NewMetadataStoreWithClientIP(metadataAddress, clientIP string) (*MetadataStore, error) {
	if metadataAddress == "" {
		metadataAddress = DefaultMetadataAddress
	}
	metadataURL := fmt.Sprintf(metadataURLTemplate, metadataAddress)

	log.Debugf("Creating new MetadataStore, metadataURL: %v, clientIP: %v", metadataURL, clientIP)
	mc, err := metadata.NewClientWithIPAndWait(metadataURL, clientIP)
	if err != nil {
		log.Errorf("couldn't create metadata client: %v", err)
		return nil, err
	}

	ms := &MetadataStore{}
	ms.mc = mc

	return ms, nil
}

// NewMetadataStore creates, intializes and returns a store for use
func NewMetadataStore(metadataAddress string) (*MetadataStore, error) {
	if metadataAddress == "" {
		metadataAddress = DefaultMetadataAddress
	}
	metadataURL := fmt.Sprintf(metadataURLTemplate, metadataAddress)

	log.Debugf("Creating new MetadataStore, metadataURL: %v", metadataURL)
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		log.Errorf("couldn't create metadata client: %v", err)
		return nil, err
	}

	ms := &MetadataStore{}
	ms.mc = mc

	return ms, nil
}

// LocalHostIPAddress returns the IP address of the host where the agent is running
func (ms *MetadataStore) LocalHostIPAddress() string {
	return ms.self.HostIPAddress
}

// LocalIPAddress returns the IP address of the current agent
func (ms *MetadataStore) LocalIPAddress() string {
	ip, _, err := net.ParseCIDR(ms.self.IPAddress)
	if err != nil {
		log.Errorf("error: %v", err)
		return ""
	}

	return ip.String()
}

// IsRemote is used to check if the given IP addresss is available on the local host or remote
func (ms *MetadataStore) IsRemote(ipAddress string) bool {
	if _, ok := ms.local[ipAddress]; ok {
		log.Debugf("Local: %s", ipAddress)
		return false
	}

	_, ok := ms.remote[ipAddress]
	if ok {
		log.Debugf("Remote: %s", ipAddress)
	}
	return ok
}

// Entries is used to get all the entries in the database
func (ms *MetadataStore) Entries() []Entry {
	return ms.entries
}

func (ms *MetadataStore) buildPeersMap() map[string]Entry {
	peersMap := make(map[string]Entry)

	for _, h := range ms.info.hosts {
		isSelf := h.UUID == ms.info.selfHost.UUID
		e := Entry{
			h.AgentIP + "/32",
			h.AgentIP,
			isSelf,
			true,
		}
		peersMap[h.AgentIP] = e
	}

	return peersMap
}

func (ms *MetadataStore) getEntryFromHost(h metadata.Host) (Entry, error) {
	isSelf := h.UUID == ms.info.selfHost.UUID

	entry := Entry{
		h.AgentIP + "/32",
		h.AgentIP,
		isSelf,
		true,
	}

	return entry, nil
}

func getSelfNetwork(networks []metadata.Network) metadata.Network {
	var selfNetwork metadata.Network
	for _, n := range networks {
		if n.Name == "ipsec" {
			selfNetwork = n
			break
		}
	}
	return selfNetwork
}

func (ms *MetadataStore) getEntryFromContainer(c metadata.Container) (Entry, error) {
	isPeer := false
	isSelf := false

	entry := Entry{
		c.PrimaryIp + ms.info.selfNetworkSubnetPrefix,
		ms.info.hostsMap[c.HostUUID].AgentIP,
		isSelf,
		isPeer,
	}

	return entry, nil
}

// RemoteEntriesMap is used to get a map of all entries which are remote
func (ms *MetadataStore) RemoteEntriesMap() map[string]Entry {
	return ms.remote
}

// PeerEntriesMap is used to get a map of entries with only the peers
func (ms *MetadataStore) PeerEntriesMap() map[string]Entry {
	return ms.peersMap
}

// RemoteNonPeerEntriesMap is used to get a map of all entries which are remote
func (ms *MetadataStore) RemoteNonPeerEntriesMap() map[string]Entry {
	return ms.remoteNonPeersMap
}

// getHostsMapFromHostsArray returns a map of hosts which can be looked up by UUID of the host
func getHostsMapFromHostsArray(hosts []metadata.Host) map[string]metadata.Host {
	hostsMap := map[string]metadata.Host{}

	for _, h := range hosts {
		log.Debugf("h: %v", h)
		hostsMap[h.UUID] = h
	}

	log.Debugf("hostsMap: %v", hostsMap)
	return hostsMap
}

func getNetworksMapFromNetworksArray(networks []metadata.Network) map[string]metadata.Network {
	networksMap := map[string]metadata.Network{}

	for _, aNetwork := range networks {
		networksMap[aNetwork.UUID] = aNetwork
	}

	log.Debugf("networksMap: %+v", networksMap)
	return networksMap
}

func (ms *MetadataStore) getLinkedFromServicesToSelf() []*metadata.Service {
	linkedTo := "ipsec/ipsec"
	log.Debugf("getLinkedFromServicesToSelf linkedTo: %v", linkedTo)

	var linkedFromServices []*metadata.Service

	for _, service := range ms.info.services {
		if !service.System {
			continue
		}
		linkedFromServiceName := service.StackName + "/" + service.Name
		if len(service.Links) > 0 {
			for linkedService := range service.Links {
				if linkedService != linkedTo {
					continue
				}
				linkedFromServices = append(linkedFromServices, ms.info.servicesMapByName[linkedFromServiceName]...)
			}
		}
	}

	log.Debugf("linkedFromServices: %v", linkedFromServices)
	return linkedFromServices
}

// When environments are linked, the network services across the
// environments are linked. This function goes through the links
// either to/from and figures out the networks of those peers.
func (ms *MetadataStore) getLinkedPeersInfo() (map[string]bool, []metadata.Container) {
	linkedPeersNetworks := map[string]bool{}
	var linkedPeersContainers []metadata.Container

	// Find out if the current service has links else if other services link to current service
	curServicePtr := ms.info.servicesMapByName["ipsec/ipsec"]
	curService := *curServicePtr[0]
	if len(curService.Links) > 0 {
		for linkedServiceName := range curService.Links {
			linkedServices, ok := ms.info.servicesMapByName[linkedServiceName]
			log.Debugf("linkedServices: %+v", linkedServices)
			if !ok {
				log.Errorf("Current service is linked to service: %v, but cannot find in servicesMapByName", linkedServiceName)
				continue
			} else {
				for _, aService := range linkedServices {
					for _, aContainer := range aService.Containers {
						if !(aContainer.State == "running" || aContainer.State == "starting") {
							continue
						}
						// Skip containers whose network names don't match self
						if ms.info.networksMap[aContainer.NetworkUUID].Name != ms.info.selfNetwork.Name {
							continue
						}
						linkedPeersContainers = append(linkedPeersContainers, aContainer)
						if _, ok := linkedPeersNetworks[aContainer.NetworkUUID]; !ok {
							linkedPeersNetworks[aContainer.NetworkUUID] = true
						}
					}
				}
			}
		}
	} else {
		linkedFromServices := ms.getLinkedFromServicesToSelf()
		for _, aService := range linkedFromServices {
			for _, aContainer := range aService.Containers {
				if !(aContainer.State == "running" || aContainer.State == "starting") {
					continue
				}
				// Skip containers whose network names don't match self
				if ms.info.networksMap[aContainer.NetworkUUID].Name != ms.info.selfNetwork.Name {
					continue
				}
				linkedPeersContainers = append(linkedPeersContainers, aContainer)
				if _, ok := linkedPeersNetworks[aContainer.NetworkUUID]; !ok {
					linkedPeersNetworks[aContainer.NetworkUUID] = true
				}
			}
		}
	}

	log.Debugf("getLinkedPeersInfo linkedPeersNetworks: %+v", linkedPeersNetworks)
	log.Debugf("getLinkedPeersInfo linkedPeersContainers: %v", linkedPeersContainers)
	return linkedPeersNetworks, linkedPeersContainers
}

func (ms *MetadataStore) doInternalRefresh() {
	log.Debugf("Doing internal refresh")

	ms.self, _ = ms.getEntryFromHost(ms.info.selfHost)

	seen := map[string]bool{}
	entries := []Entry{}
	local := map[string]Entry{}
	remote := map[string]Entry{}
	remoteNonPeersMap := map[string]Entry{}
	//peersNetworks, linkedPeersContainers := ms.getLinkedPeersInfo()

	peersMap := ms.buildPeersMap()

	for _, c := range ms.info.containers {
		if !(c.State == "running" || c.State == "starting") {
			continue
		}

		// TODO:
		// check if the container networkUUID is part of peersNetworks
		//_, isPresentInPeersNetworks := peersNetworks[c.NetworkUUID]

		//if !isPresentInPeersNetworks ||
		if c.PrimaryIp == "" ||
			c.NetworkFromContainerUUID != "" ||
			c.NetworkUUID != ms.info.selfNetwork.UUID ||
			c.PrimaryIp == ms.info.selfHost.AgentIP ||
			c.PrimaryIp == ms.info.hostsMap[c.HostUUID].AgentIP {
			continue
		}

		log.Debugf("Getting Entry from Container: %+v", c)
		e, _ := ms.getEntryFromContainer(c)

		ipNoCidr := strings.Split(e.IPAddress, "/")[0]

		if seen[ipNoCidr] {
			continue
		}
		seen[ipNoCidr] = true

		if e.HostIPAddress == ms.self.HostIPAddress {
			local[ipNoCidr] = e
		} else {
			remote[ipNoCidr] = e
			if !e.Peer {
				remoteNonPeersMap[ipNoCidr] = e
			}
		}

		log.Debugf("entry: %+v", e)
		entries = append(entries, e)
	}

	log.Debugf("entries: %+v", entries)
	log.Debugf("peersMap: %+v", peersMap)
	log.Debugf("local: %+v", local)
	log.Debugf("remote: %+v", remote)

	ms.entries = entries
	ms.peersMap = peersMap
	ms.local = local
	ms.remote = remote
	ms.remoteNonPeersMap = remoteNonPeersMap
}

// getServicesMapByName builds a map indexed by `stack_name/service_name`
// It excludes the current service in the map
func getServicesMapByName(services []metadata.Service) map[string][]*metadata.Service {
	// Build serviceMap by "stack_name/service_name"
	// The reason for an array in map value is because of not
	// using UUID but names which can result in duplicates.
	// TODO: Once LinksByUUID is available, use that instead
	servicesMapByName := make(map[string][]*metadata.Service)
	for index, aService := range services {
		if !aService.System {
			continue
		}
		key := aService.StackName + "/" + aService.Name
		if value, ok := servicesMapByName[key]; ok {
			servicesMapByName[key] = append(value, &services[index])

		} else {
			servicesMapByName[key] = []*metadata.Service{&services[index]}
		}
	}
	log.Debugf("servicesMapByName: %+v", servicesMapByName)

	return servicesMapByName
}

func getSubnetPrefixFromNetworkConfig(network metadata.Network) string {
	conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	for _, file := range conf {
		props, _ := file.(map[string]interface{})
		ipamConf, found := props["ipam"].(map[string]interface{})
		if !found {
			log.Errorf("couldn't find ipam key in network config")
			return defaultSubnetPrefix
		}

		sp, found := ipamConf["subnetPrefixSize"].(string)
		if !found {
			log.Errorf("couldn't find subnetPrefixSize in network ipam config")
			return defaultSubnetPrefix
		}
		return sp
	}
	return defaultSubnetPrefix
}

// Reload is used to refresh/reload the data from metadata
func (ms *MetadataStore) Reload() error {
	log.Debugf("Reloading ...")

	selfHost, err := ms.mc.GetSelfHost()
	if err != nil {
		log.Errorf("couldn't get self host from metadata: %v", err)
		return err
	}

	hosts, err := ms.mc.GetHosts()
	if err != nil {
		log.Errorf("couldn't get hosts from metadata: %v", err)
		return err
	}

	containers, err := ms.mc.GetContainers()
	if err != nil {
		log.Errorf("couldn't get containers from metadata: %v", err)
		return err
	}

	services, err := ms.mc.GetServices()
	if err != nil {
		log.Errorf("couldn't get services from metadata: %v", err)
		return err
	}

	servicesMapByName := getServicesMapByName(services)

	hostsMap := getHostsMapFromHostsArray(hosts)

	networks, err := ms.mc.GetNetworks()
	if err != nil {
		log.Errorf("couldn't get networks from metadata: %v", err)
		return err
	}
	networksMap := getNetworksMapFromNetworksArray(networks)

	selfNetwork := getSelfNetwork(networks)

	selfNetworkSubnetPrefix := getSubnetPrefixFromNetworkConfig(selfNetwork)

	info := &InfoFromMetadata{
		selfHost,
		selfNetwork,
		selfNetworkSubnetPrefix,
		services,
		servicesMapByName,
		hosts,
		containers,
		hostsMap,
		networksMap,
	}

	ms.info = info

	ms.doInternalRefresh()

	return nil
}
