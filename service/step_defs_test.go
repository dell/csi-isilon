package service

/*
 Copyright (c) 2019 Dell Inc, or its subsidiaries.

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
import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"

	"github.com/DATA-DOG/godog"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/csi-isilon/common/utils"
	"github.com/dell/gofsutil"
	"github.com/rexray/gocsi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

type feature struct {
	nGoRoutines                        int
	server                             *httptest.Server
	service                            *service
	err                                error // return from the preceeding call
	getPluginInfoResponse              *csi.GetPluginInfoResponse
	getPluginCapabilitiesResponse      *csi.GetPluginCapabilitiesResponse
	probeResponse                      *csi.ProbeResponse
	createVolumeResponse               *csi.CreateVolumeResponse
	publishVolumeResponse              *csi.ControllerPublishVolumeResponse
	unpublishVolumeResponse            *csi.ControllerUnpublishVolumeResponse
	nodeGetInfoResponse                *csi.NodeGetInfoResponse
	nodeGetCapabilitiesResponse        *csi.NodeGetCapabilitiesResponse
	deleteVolumeResponse               *csi.DeleteVolumeResponse
	getCapacityResponse                *csi.GetCapacityResponse
	controllerGetCapabilitiesResponse  *csi.ControllerGetCapabilitiesResponse
	validateVolumeCapabilitiesResponse *csi.ValidateVolumeCapabilitiesResponse
	createSnapshotResponse             *csi.CreateSnapshotResponse
	createVolumeRequest                *csi.CreateVolumeRequest
	publishVolumeRequest               *csi.ControllerPublishVolumeRequest
	unpublishVolumeRequest             *csi.ControllerUnpublishVolumeRequest
	deleteVolumeRequest                *csi.DeleteVolumeRequest
	listVolumesRequest                 *csi.ListVolumesRequest
	listVolumesResponse                *csi.ListVolumesResponse
	listSnapshotsRequest               *csi.ListSnapshotsRequest
	listSnapshotsResponse              *csi.ListSnapshotsResponse
	listedVolumeIDs                    map[string]bool
	listVolumesNextTokenCache          string
	wrongCapacity, wrongStoragePool    bool
	accessZone                         string
	capability                         *csi.VolumeCapability
	capabilities                       []*csi.VolumeCapability
	nodeStageVolumeRequest             *csi.NodeStageVolumeRequest
	nodeStageVolumeResponse            *csi.NodeStageVolumeResponse
	nodeUnstageVolumeRequest           *csi.NodeUnstageVolumeRequest
	nodeUnstageVolumeResponse          *csi.NodeUnstageVolumeResponse
	nodePublishVolumeRequest           *csi.NodePublishVolumeRequest
	nodeUnpublishVolumeRequest         *csi.NodeUnpublishVolumeRequest
	nodeUnpublishVolumeResponse        *csi.NodeUnpublishVolumeResponse
	deleteSnapshotRequest              *csi.DeleteSnapshotRequest
	deleteSnapshotResponse             *csi.DeleteSnapshotResponse
	createSnapshotRequest              *csi.CreateSnapshotRequest
	volumeIDList                       []string
	snapshotIDList                     []string
	snapshotIndex                      int
}

var inducedErrors struct {
	badVolumeIdentifier  bool
	invalidVolumeID      bool
	noVolumeID           bool
	differentVolumeID    bool
	noNodeName           bool
	noNodeID             bool
	omitVolumeCapability bool
	omitAccessMode       bool
	useAccessTypeMount   bool
	noIsiService         bool
	autoProbeNotEnabled  bool
}

const (
	Volume1   = "d0f055a700000000"
	datafile  = "test/tmp/datafile"
	datadir   = "test/tmp/datadir"
	datafile2 = "test/tmp/datafile2"
	datadir2  = "test/tmp/datadir2"
)

func (f *feature) aIsilonService() error {
	f.checkGoRoutines("start aIsilonService")

	f.err = nil
	f.getPluginInfoResponse = nil
	f.volumeIDList = f.volumeIDList[:0]
	f.snapshotIDList = f.snapshotIDList[:0]

	// configure gofsutil; we use a mock interface
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceBindMountError = false
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceGetMountsError = false
	gofsutil.GOFSMock.InduceDevMountsError = false
	gofsutil.GOFSMock.InduceUnmountError = false
	gofsutil.GOFSMock.InduceFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatType = ""
	gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]

	// set induced errors
	inducedErrors.badVolumeIdentifier = false
	inducedErrors.invalidVolumeID = false
	inducedErrors.noVolumeID = false
	inducedErrors.differentVolumeID = false
	inducedErrors.noNodeName = false
	inducedErrors.noNodeID = false
	inducedErrors.omitVolumeCapability = false
	inducedErrors.omitAccessMode = false

	// initialize volume and export existence status
	stepHandlersErrors.ExportNotFoundError = true
	stepHandlersErrors.VolumeNotExistError = true

	// Get the httptest mock handler. Only set
	// a new server if there isn't one already.
	handler := getHandler()
	// Get or reuse the cached service
	f.getService()
	if handler != nil && os.Getenv("CSI_ISILON_ENDPOINT") == "" {
		if f.server == nil {
			f.server = httptest.NewServer(handler)
		}
		log.Printf("server url: %s\n", f.server.URL)
		f.service.opts.EndpointURL = f.server.URL
	} else {
		f.server = nil
	}
	f.service.isiSvc, _ = f.service.GetIsiService(context.Background())
	f.checkGoRoutines("end aIsilonService")
	f.service.logServiceStats()
	return nil
}

func (f *feature) renderOneFSAPIUnreachable() error {
	testControllerHasNoConnection = true
	testNodeHasNoConnection = true
	return nil
}

func (f *feature) enableQuota() error {
	f.service.opts.QuotaEnabled = true
	return nil
}

func (f *feature) getService() *service {
	testControllerHasNoConnection = false
	testNodeHasNoConnection = false
	svc := new(service)
	var opts Opts
	opts.User = "blah"
	opts.Password = "blah"
	opts.Endpoint = "127.0.0.1"
	opts.EndpointURL = "http://127.0.0.1"
	opts.AccessZone = "System"
	if os.Getenv("CSI_ISILON_ENDPOINT") != "" {
		opts.EndpointURL = os.Getenv("CSI_ISILON_ENDPOINT")
	}
	if os.Getenv("CSI_ISILON_USERID") != "" {
		opts.User = os.Getenv("CSI_ISILON_USERID")
	}
	if os.Getenv("CSI_ISILON_PASSWORD") != "" {
		opts.Password = os.Getenv("CSI_ISILON_PASSWORD")
	}
	if os.Getenv("CSI_ISILON_PATH") != "" {
		opts.Path = os.Getenv("CSI_ISILON_PATH")
	}
	if os.Getenv("CSI_ISILON_ZONE") != "" {
		opts.AccessZone = os.Getenv("CSI_ISILON_ZONE")
	}

	opts.Path = "/ifs/data/csi-isilon"
	opts.Insecure = true
	opts.DebugEnabled = true
	opts.Verbose = 1
	svc.opts = opts
	svc.mode = "controller"
	f.service = svc
	f.service.nodeID = "k8s-rhel76-qual-10-247-98-140"
	f.service.nodeIP = "10.247.98.140"

	utils.ConfigureLogger(opts.DebugEnabled)

	return svc
}

func (f *feature) iSetEmptyPassword() error {
	f.service.opts.Password = ""
	return nil
}

func (f *feature) checkGoRoutines(tag string) {
	goroutines := runtime.NumGoroutine()
	fmt.Printf("goroutines %s new %d old groutines %d\n", tag, goroutines, f.nGoRoutines)
	f.nGoRoutines = goroutines
}

func FeatureContext(s *godog.Suite) {
	f := &feature{}
	s.Step(`^a Isilon service$`, f.aIsilonService)
	s.Step(`^a Isilon service with params "([^"]*)" "([^"]*)"$`, f.aIsilonServiceWithParams)
	s.Step(`^I render Isilon service unreachable$`, f.renderOneFSAPIUnreachable)
	s.Step(`^I enable quota$`, f.enableQuota)
	s.Step(`^I call GetPluginInfo$`, f.iCallGetPluginInfo)
	s.Step(`^a valid GetPlugInfoResponse is returned$`, f.aValidGetPlugInfoResponseIsReturned)
	s.Step(`^I call GetPluginCapabilities$`, f.iCallGetPluginCapabilities)
	s.Step(`^a valid GetPluginCapabilitiesResponse is returned$`, f.aValidGetPluginCapabilitiesResponseIsReturned)
	s.Step(`^I call Probe$`, f.iCallProbe)
	s.Step(`^I call autoProbe$`, f.iCallAutoProbe)
	s.Step(`^a valid ProbeResponse is returned$`, f.aValidProbeResponseIsReturned)
	s.Step(`^an invalid ProbeResponse is returned$`, f.anInvalidProbeResponseIsReturned)
	s.Step(`^I set empty password for Isilon service$`, f.iSetEmptyPassword)
	s.Step(`^I call CreateVolume "([^"]*)"$`, f.iCallCreateVolume)
	s.Step(`^I call CreateVolume with params "([^"]*)" (-?\d+) "([^"]*)" "([^"]*)" "([^"]*)"$`, f.iCallCreateVolumeWithParams)
	s.Step(`^I call DeleteVolume "([^"]*)"$`, f.iCallDeleteVolume)
	s.Step(`^a valid CreateVolumeResponse is returned$`, f.aValidCreateVolumeResponseIsReturned)
	s.Step(`^a valid DeleteVolumeResponse is returned$`, f.aValidDeleteVolumeResponseIsReturned)
	s.Step(`^I induce error "([^"]*)"$`, f.iInduceError)
	s.Step(`^the error contains "([^"]*)"$`, f.theErrorContains)
	s.Step(`^I call ControllerGetCapabilities$`, f.iCallControllerGetCapabilities)
	s.Step(`^a valid ControllerGetCapabilitiesResponse is returned$`, f.aValidControllerGetCapabilitiesResponseIsReturned)
	s.Step(`^I call ValidateVolumeCapabilities with voltype "([^"]*)" access "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVoltypeAccess)
	s.Step(`^I call GetCapacity$`, f.iCallGetCapacity)
	s.Step(`^a valid GetCapacityResponse is returned$`, f.aValidGetCapacityResponseIsReturned)
	s.Step(`^I call GetCapacity with Invalid access mode$`, f.iCallGetCapacityWithInvalidAccessMode)
	s.Step(`^I call NodeGetInfo$`, f.iCallNodeGetInfo)
	s.Step(`^a valid NodeGetInfoResponse is returned$`, f.aValidNodeGetInfoResponseIsReturned)
	s.Step(`^I call NodeGetCapabilities$`, f.iCallNodeGetCapabilities)
	s.Step(`^a valid NodeGetCapabilitiesResponse is returned$`, f.aValidNodeGetCapabilitiesResponseIsReturned)
	s.Step(`^I have a Node "([^"]*)" with AccessZone$`, f.iHaveANodeWithAccessZone)
	s.Step(`^I call ControllerPublishVolume with "([^"]*)" to "([^"]*)"$`, f.iCallControllerPublishVolumeWithTo)
	s.Step(`^a valid ControllerPublishVolumeResponse is returned$`, f.aValidControllerPublishVolumeResponseIsReturned)
	s.Step(`^a controller published volume$`, f.aControllerPublishedVolume)
	s.Step(`^a capability with voltype "([^"]*)" access "([^"]*)"$`, f.aCapabilityWithVoltypeAccess)
	s.Step(`^I call NodePublishVolume$`, f.iCallNodePublishVolume)
	s.Step(`^get Node Publish Volume Request$`, f.getNodePublishVolumeRequest)
	s.Step(`^I change the target path$`, f.iChangeTheTargetPath)
	s.Step(`^I mark request read only$`, f.iMarkRequestReadOnly)
	s.Step(`^I call NodeStageVolume with name "([^"]*)" and access type "([^"]*)"$`, f.iCallNodeStageVolume)
	s.Step(`^a valid NodeStageVolumeResponse is returned$`, f.aValidNodeStageVolumeResponseIsReturned)
	s.Step(`^I call NodeUnstageVolume with name "([^"]*)"$`, f.iCallNodeUnstageVolume)
	s.Step(`^a valid NodeUnstageVolumeResponse is returned$`, f.aValidNodeUnstageVolumeResponseIsReturned)
	s.Step(`^I call ListVolumes with max entries (-?\d+) starting token "([^"]*)"$`, f.iCallListVolumesWithMaxEntriesStartingToken)
	s.Step(`^a valid ListVolumesResponse is returned$`, f.aValidListVolumesResponseIsReturned)
	s.Step(`^I call NodeUnpublishVolume$`, f.iCallNodeUnpublishVolume)
	s.Step(`^a valid NodeUnpublishVolumeResponse is returned$`, f.aValidNodeUnpublishVolumeResponseIsReturned)
	s.Step(`^I call CreateSnapshot "([^"]*)" "([^"]*)" "([^"]*)"$`, f.iCallCreateSnapshot)
	s.Step(`^a valid CreateSnapshotResponse is returned$`, f.aValidCreateSnapshotResponseIsReturned)
	s.Step(`^I call DeleteSnapshot "([^"]*)"$`, f.iCallDeleteSnapshot)
	s.Step(`^I call CreateVolumeFromSnapshot "([^"]*)" "([^"]*)"$`, f.iCallCreateVolumeFromSnapshot)
	s.Step(`^I call CreateVolumeFromVolume "([^"]*)" "([^"]*)"$`, f.iCallCreateVolumeFromVolume)
	s.Step(`^I call initialize real isilon service$`, f.iCallInitializeRealIsilonService)
	s.Step(`^I call logStatistics (\d+) times$`, f.iCallLogStatisticsTimes)
	s.Step(`^I call BeforeServe$`, f.iCallBeforeServe)
	s.Step(`^I call CreateQuota in isiService with negative sizeInBytes$`, f.ICallCreateQuotaInIsiServiceWithNegativeSizeInBytes)
	s.Step(`^I call get export related functions in isiService$`, f.iCallGetExportRelatedFunctionsInIsiService)
	s.Step(`^I call unimplemented functions$`, f.iCallUnimplementedFunctions)
	s.Step(`^I call init Service object$`, f.iCallInitServiceObject)
}

// GetPluginInfo
func (f *feature) iCallGetPluginInfo() error {
	ctx := new(context.Context)
	req := new(csi.GetPluginInfoRequest)
	f.getPluginInfoResponse, f.err = f.service.GetPluginInfo(*ctx, req)
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) aValidGetPlugInfoResponseIsReturned() error {
	rep := f.getPluginInfoResponse
	url := rep.GetManifest()["url"]
	if rep.GetName() == "" || rep.GetVendorVersion() == "" || url == "" {
		return errors.New("Expected GetPluginInfo to return name and version")
	}
	log.Printf("Name %s Version %s URL %s", rep.GetName(), rep.GetVendorVersion(), url)
	return nil
}

func (f *feature) iCallGetPluginCapabilities() error {
	ctx := new(context.Context)
	req := new(csi.GetPluginCapabilitiesRequest)
	f.getPluginCapabilitiesResponse, f.err = f.service.GetPluginCapabilities(*ctx, req)
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) aValidGetPluginCapabilitiesResponseIsReturned() error {
	rep := f.getPluginCapabilitiesResponse
	capabilities := rep.GetCapabilities()
	var foundController bool
	for _, capability := range capabilities {
		if capability.GetService().GetType() == csi.PluginCapability_Service_CONTROLLER_SERVICE {
			foundController = true
		}
	}
	if !foundController {
		return errors.New("Expected PluginCapabilitiesResponse to contain CONTROLLER_SERVICE")
	}
	return nil
}

func (f *feature) iCallProbe() error {
	ctx := new(context.Context)
	req := new(csi.ProbeRequest)
	f.checkGoRoutines("before probe")
	f.probeResponse, f.err = f.service.Probe(*ctx, req)
	f.checkGoRoutines("after probe")
	return nil
}

func (f *feature) iCallAutoProbe() error {
	f.checkGoRoutines("before auto probe")
	f.err = f.service.autoProbe(context.Background())
	f.checkGoRoutines("after auto probe")
	return nil
}

func (f *feature) aValidProbeResponseIsReturned() error {
	if f.probeResponse.GetReady().GetValue() != true {
		return errors.New("Probe returned 'Ready': false")
	}
	return nil
}

func (f *feature) anInvalidProbeResponseIsReturned() error {
	if f.probeResponse.GetReady().GetValue() != false {
		return errors.New("Probe returned 'Ready': true")
	}
	return nil
}

func getTypicalCreateVolumeRequest() *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	req.Name = "volume1"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	mount := new(csi.VolumeCapability_MountVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Mount)
	accessType.Mount = mount
	capability.AccessType = accessType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	parameters := make(map[string]string)
	parameters[AccessZoneParam] = "System"
	parameters[IsiPathParam] = "/ifs/data/csi-isilon"
	req.Parameters = parameters
	req.VolumeCapabilities = capabilities
	return req
}

func getCreateVolumeRequestWithParams(rangeInGiB int64, accessZone, isiPath, AzServiceIP string) *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	req.Name = "volume1"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = rangeInGiB * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	mount := new(csi.VolumeCapability_MountVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Mount)
	accessType.Mount = mount
	capability.AccessType = accessType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	parameters := make(map[string]string)
	if accessZone != "none" {
		parameters[AccessZoneParam] = accessZone
	}
	if isiPath != "none" {
		parameters[IsiPathParam] = isiPath
	}
	if AzServiceIP != "none" {
		parameters[AzServiceIPParam] = AzServiceIP
	}
	req.Parameters = parameters
	req.VolumeCapabilities = capabilities
	return req
}

func getTypicalDeleteVolumeRequest() *csi.DeleteVolumeRequest {
	req := new(csi.DeleteVolumeRequest)
	req.VolumeId = "volume1"
	return req
}

func getTypicalNodeStageVolumeRequest(accessType string) *csi.NodeStageVolumeRequest {
	req := new(csi.NodeStageVolumeRequest)
	volCtx := make(map[string]string)
	req.VolumeContext = volCtx
	req.VolumeId = "volume2"

	capability := new(csi.VolumeCapability)

	if !inducedErrors.omitAccessMode {
		capability.AccessMode = getAccessMode(accessType)
	}

	req.VolumeCapability = capability

	return req
}

func getTypicalNodeUnstageVolumeRequest(volID string) *csi.NodeUnstageVolumeRequest {
	req := new(csi.NodeUnstageVolumeRequest)
	req.VolumeId = volID
	return req
}

func getAccessMode(accessType string) *csi.VolumeCapability_AccessMode {

	accessMode := new(csi.VolumeCapability_AccessMode)
	switch accessType {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "single-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}

	return accessMode
}

func (f *feature) iCallCreateVolume(name string) error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	f.createVolumeRequest = req
	req.Name = name
	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume call failed: %s\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
		stepHandlersErrors.ExportNotFoundError = false
		stepHandlersErrors.VolumeNotExistError = false
	}
	return nil
}

func (f *feature) iCallCreateVolumeWithParams(name string, rangeInGiB int, accessZone, isiPath, AzServiceIP string) error {
	ctx := new(context.Context)
	req := getCreateVolumeRequestWithParams(int64(rangeInGiB), accessZone, isiPath, AzServiceIP)
	f.createVolumeRequest = req
	req.Name = name
	stepHandlersErrors.ExportNotFoundError = true
	stepHandlersErrors.VolumeNotExistError = true
	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume call failed: %s\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
		stepHandlersErrors.ExportNotFoundError = false
		stepHandlersErrors.VolumeNotExistError = false
	}
	return nil
}

func (f *feature) iCallDeleteVolume(name string) error {
	ctx := new(context.Context)
	if f.deleteVolumeRequest == nil {
		req := getTypicalDeleteVolumeRequest()
		f.deleteVolumeRequest = req
	}
	req := f.deleteVolumeRequest
	req.VolumeId = name

	f.deleteVolumeResponse, f.err = f.service.DeleteVolume(*ctx, req)
	if f.err != nil {
		log.Printf("DeleteVolume call failed: '%v'\n", f.err)
	}

	return nil
}

func (f *feature) aValidCreateVolumeResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	f.volumeIDList = append(f.volumeIDList, f.createVolumeResponse.Volume.VolumeId)
	fmt.Printf("volume '%s'\n",
		f.createVolumeResponse.Volume.VolumeContext["Name"])
	return nil
}

func (f *feature) aValidDeleteVolumeResponseIsReturned() error {

	if f.err != nil {
		return f.err
	}

	return nil
}

func (f *feature) iInduceError(errtype string) error {

	log.Printf("set induce error %s\n", errtype)
	switch errtype {
	case "InstancesError":
		stepHandlersErrors.InstancesError = true
	case "VolInstanceError":
		stepHandlersErrors.VolInstanceError = true
	case "StatsError":
		stepHandlersErrors.StatsError = true
	case "NoNodeID":
		inducedErrors.noNodeID = true
	case "OmitVolumeCapability":
		inducedErrors.omitVolumeCapability = true
	case "noIsiService":
		inducedErrors.noIsiService = true
	case "autoProbeNotEnabled":
		inducedErrors.autoProbeNotEnabled = true
	case "autoProbeFailed":
		f.service.isiSvc = nil
		f.service.opts.AutoProbe = false
	case "GOFSMockDevMountsError":
		gofsutil.GOFSMock.InduceDevMountsError = true
	case "GOFSMockMountError":
		gofsutil.GOFSMock.InduceMountError = true
	case "GOFSMockGetMountsError":
		gofsutil.GOFSMock.InduceGetMountsError = true
	case "GOFSMockUnmountError":
		gofsutil.GOFSMock.InduceUnmountError = true
	case "GOFSMockGetDiskFormatError":
		gofsutil.GOFSMock.InduceGetDiskFormatError = true
	case "GOFSMockGetDiskFormatType":
		gofsutil.GOFSMock.InduceGetDiskFormatType = "unknown-fs"
	case "GOFSMockFormatError":
		gofsutil.GOFSMock.InduceFormatError = true
	case "GOFSWWNToDevicePathError":
		gofsutil.GOFSMock.InduceWWNToDevicePathError = true
	case "GOFSRmoveBlockDeviceError":
		gofsutil.GOFSMock.InduceRemoveBlockDeviceError = true
	case "NodePublishNoTargetPath":
		f.nodePublishVolumeRequest.TargetPath = ""
	case "NodeUnpublishNoTargetPath":
		f.nodeUnpublishVolumeRequest.TargetPath = ""
	case "NodePublishNoVolumeCapability":
		f.nodePublishVolumeRequest.VolumeCapability = nil
	case "NodePublishNoAccessMode":
		f.nodePublishVolumeRequest.VolumeCapability.AccessMode = nil
	case "NodePublishNoAccessType":
		f.nodePublishVolumeRequest.VolumeCapability.AccessType = nil
	case "NodePublishFileTargetNotDir":
		f.nodePublishVolumeRequest.TargetPath = datafile
	case "BadVolumeIdentifier":
		inducedErrors.badVolumeIdentifier = true
	case "TargetNotCreatedForNodePublish":
		err := os.Remove(datafile)
		if err != nil {
			return nil
		}
		//cmd := exec.Command("rm", "-rf", datadir)
		//_, err = cmd.CombinedOutput()
		err = os.RemoveAll(datadir)
		if err != nil {
			return err
		}
	case "OmitAccessMode":
		inducedErrors.omitAccessMode = true
	case "TargetNotCreatedForNodeUnpublish":
		err := os.RemoveAll(datadir)
		if err != nil {
			return nil
		}
	case "GetSnapshotError":
		stepHandlersErrors.GetSnapshotError = true
	case "DeleteSnapshotError":
		stepHandlersErrors.DeleteSnapshotError = true
	case "CreateQuotaError":
		stepHandlersErrors.CreateQuotaError = true
	case "CreateExportError":
		stepHandlersErrors.CreateExportError = true
	case "GetExportInternalError":
		stepHandlersErrors.GetExportInternalError = true
	case "VolumeNotExistError":
		stepHandlersErrors.VolumeNotExistError = true
	case "ExportNotFoundError":
		stepHandlersErrors.ExportNotFoundError = true
	case "VolumeExists":
		stepHandlersErrors.VolumeNotExistError = false
	case "ExportExists":
		stepHandlersErrors.ExportNotFoundError = false
	case "ControllerHasNoConnectionError":
		testControllerHasNoConnection = true
	case "NodeHasNoConnectionError":
		testNodeHasNoConnection = true
	case "GetExportByIDNotFoundError":
		stepHandlersErrors.GetExportByIDNotFoundError = true
	case "UnexportError":
		stepHandlersErrors.UnexportError = true
	case "CreateSnapshotError":
		stepHandlersErrors.CreateSnapshotError = true
	case "DeleteQuotaError":
		stepHandlersErrors.DeleteQuotaError = true
	case "QuotaNotFoundError":
		stepHandlersErrors.QuotaNotFoundError = true
	case "DeleteVolumeError":
		stepHandlersErrors.DeleteVolumeError = true
	case "none":

	default:
		return fmt.Errorf("Don't know how to induce error %q", errtype)
	}
	return nil
}

func (f *feature) theErrorContains(arg1 string) error {
	// If arg1 is none, we expect no error, any error received is unexpected
	clearErrors()
	if arg1 == "none" {
		if f.err == nil {
			return nil
		}
		return fmt.Errorf("Unexpected error: %s", f.err)
	}
	// We expected an error...
	if f.err == nil {
		return fmt.Errorf("Expected error to contain %s but no error", arg1)
	}
	// Allow for multiple possible matches, separated by @@. This was necessary
	// because Windows and Linux sometimes return different error strings for
	// gofsutil operations. Note @@ was used instead of || because the Gherkin
	// parser is not smart enough to ignore vertical braces within a quoted string,
	// so if || is used it thinks the row's cell count is wrong.
	possibleMatches := strings.Split(arg1, "@@")
	for _, possibleMatch := range possibleMatches {
		if strings.Contains(f.err.Error(), possibleMatch) {
			return nil
		}
	}
	return fmt.Errorf("Expected error to contain %s but it was %s", arg1, f.err.Error())
}

func (f *feature) iCallControllerGetCapabilities() error {
	ctx := new(context.Context)
	req := new(csi.ControllerGetCapabilitiesRequest)
	f.controllerGetCapabilitiesResponse, f.err = f.service.ControllerGetCapabilities(*ctx, req)
	if f.err != nil {
		log.Printf("ControllerGetCapabilities call failed: %s\n", f.err.Error())
		return f.err
	}
	return nil
}

func (f *feature) aValidControllerGetCapabilitiesResponseIsReturned() error {
	rep := f.controllerGetCapabilitiesResponse
	if rep != nil {
		if rep.Capabilities == nil {
			return errors.New("no capabilities returned in ControllerGetCapabilitiesResponse")
		}
		count := 0
		for _, cap := range rep.Capabilities {
			rpcType := cap.GetRpc().Type
			switch rpcType {
			case csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_LIST_VOLUMES:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_GET_CAPACITY:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS:
				count = count + 1
			default:
				return fmt.Errorf("received unexpected capability: %v", rpcType)
			}
		}
		if count != 4 /*6*/ {
			return errors.New("Did not retrieve all the expected capabilities")
		}
		return nil
	}
	return errors.New("expected ControllerGetCapabilitiesResponse but didn't get one")
}

func (f *feature) iCallValidateVolumeCapabilitiesWithVoltypeAccess(voltype, access string) error {
	ctx := new(context.Context)
	req := new(csi.ValidateVolumeCapabilitiesRequest)
	if inducedErrors.invalidVolumeID || f.createVolumeResponse == nil {
		req.VolumeId = "000-000"
	} else {
		req.VolumeId = f.createVolumeResponse.GetVolume().VolumeId
	}
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	switch voltype {
	case "block":
		block := new(csi.VolumeCapability_BlockVolume)
		accessType := new(csi.VolumeCapability_Block)
		accessType.Block = block
		capability.AccessType = accessType
	case "mount":
		mount := new(csi.VolumeCapability_MountVolume)
		accessType := new(csi.VolumeCapability_Mount)
		accessType.Mount = mount
		capability.AccessType = accessType
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch access {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "single-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	case "multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multi-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multi-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	log.Printf("Calling ValidateVolumeCapabilities")
	f.validateVolumeCapabilitiesResponse, f.err = f.service.ValidateVolumeCapabilities(*ctx, req)
	if f.err != nil {
		return nil
	}
	if f.validateVolumeCapabilitiesResponse.Message != "" {
		f.err = errors.New(f.validateVolumeCapabilitiesResponse.Message)
	} else {
		// Validate we get a Confirmed structure with VolumeCapabilities
		if f.validateVolumeCapabilitiesResponse.Confirmed == nil {
			return errors.New("Expected ValidateVolumeCapabilities to have a Confirmed structure but it did not")
		}
		confirmed := f.validateVolumeCapabilitiesResponse.Confirmed
		if len(confirmed.VolumeCapabilities) <= 0 {
			return errors.New("Expected ValidateVolumeCapabilities to return the confirmed VolumeCapabilities but it did not")
		}
	}
	return nil
}

func clearErrors() {
	stepHandlersErrors.ExportNotFoundError = true
	stepHandlersErrors.VolumeNotExistError = true
	stepHandlersErrors.InstancesError = false
	stepHandlersErrors.VolInstanceError = false
	stepHandlersErrors.FindVolumeIDError = false
	stepHandlersErrors.GetVolByIDError = false
	stepHandlersErrors.GetStoragePoolsError = false
	stepHandlersErrors.GetStatisticsError = false
	stepHandlersErrors.CreateSnapshotError = false
	stepHandlersErrors.RemoveVolumeError = false
	stepHandlersErrors.StatsError = false
	stepHandlersErrors.StartingTokenInvalidError = false
	stepHandlersErrors.GetSnapshotError = false
	stepHandlersErrors.DeleteSnapshotError = false
	stepHandlersErrors.ExportNotFoundError = false
	stepHandlersErrors.VolumeNotExistError = false
	stepHandlersErrors.CreateQuotaError = false
	stepHandlersErrors.CreateExportError = false
	stepHandlersErrors.GetExportInternalError = false
	stepHandlersErrors.GetExportByIDNotFoundError = false
	stepHandlersErrors.UnexportError = false
	stepHandlersErrors.DeleteQuotaError = false
	stepHandlersErrors.QuotaNotFoundError = false
	stepHandlersErrors.DeleteVolumeError = false
	inducedErrors.noIsiService = false
	inducedErrors.autoProbeNotEnabled = false
}

func getTypicalCapacityRequest(valid bool) *csi.GetCapacityRequest {
	req := new(csi.GetCapacityRequest)
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	// Set FS type to mount volume
	mount := new(csi.VolumeCapability_MountVolume)
	accessType := new(csi.VolumeCapability_Mount)
	accessType.Mount = mount
	capability.AccessType = accessType
	// A single mode writer
	accessMode := new(csi.VolumeCapability_AccessMode)
	if valid {
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	} else {
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	return req
}

func (f *feature) iCallGetCapacity() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCapacityRequest(true)
	f.getCapacityResponse, f.err = f.service.GetCapacity(ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) iCallGetCapacityWithInvalidAccessMode() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := getTypicalCapacityRequest(false)
	f.getCapacityResponse, f.err = f.service.GetCapacity(ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) aValidGetCapacityResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.getCapacityResponse == nil {
		return errors.New("Received null response to GetCapacity")
	}
	if f.getCapacityResponse.AvailableCapacity <= 0 {
		return errors.New("Expected AvailableCapacity to be positive")
	}
	fmt.Printf("Available capacity: %d\n", f.getCapacityResponse.AvailableCapacity)

	return nil
}

func (f *feature) iCallNodeGetInfo() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetInfoRequest)
	f.nodeGetInfoResponse, f.err = f.service.NodeGetInfo(*ctx, req)
	if f.err != nil {
		log.Printf("NodeGetInfo call failed: %s\n", f.err.Error())
		return f.err
	}
	return nil
}

func (f *feature) iCallNodeGetCapabilities() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetCapabilitiesRequest)
	f.nodeGetCapabilitiesResponse, f.err = f.service.NodeGetCapabilities(*ctx, req)
	if f.err != nil {
		log.Printf("NodeGetCapabilities call failed: %s\n", f.err.Error())
		return f.err
	}
	return nil
}

func (f *feature) aValidNodeGetInfoResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	fmt.Printf("The node ID is %s\n", f.nodeGetInfoResponse.NodeId)
	return nil
}

func (f *feature) aValidNodeGetCapabilitiesResponseIsReturned() error {
	rep := f.nodeGetCapabilitiesResponse
	if rep != nil {
		if rep.Capabilities == nil {
			return errors.New("No capabilities returned in NodeGetCapabilitiesResponse")
		}
		count := 0
		for _, cap := range rep.Capabilities {
			rpcType := cap.GetRpc().Type
			switch rpcType {
			case csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME:
				count = count + 1
			case csi.NodeServiceCapability_RPC_GET_VOLUME_STATS:
				count = count + 1
			case csi.NodeServiceCapability_RPC_EXPAND_VOLUME:
				count = count + 1
			default:
				return fmt.Errorf("Received unexpected capability: %v", rpcType)
			}
		}
		if count != 1 /*3*/ {
			return errors.New("Did not retrieve all the expected capabilities")
		}
		return nil
	}
	return errors.New("Expected NodeGetCapabilitiesResponse but didn't get one")
}

func (f *feature) iHaveANodeWithAccessZone(nodeID string) error {
	f.accessZone = "CSI-" + nodeID
	return nil
}

func (f *feature) iCallControllerPublishVolumeWithTo(accessMode, nodeID string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.publishVolumeRequest
	if f.publishVolumeRequest == nil {
		req = f.getControllerPublishVolumeRequest(accessMode, nodeID)
		f.publishVolumeRequest = req
	}
	log.Printf("Calling controllerPublishVolume")
	f.publishVolumeResponse, f.err = f.service.ControllerPublishVolume(ctx, req)
	if f.err != nil {
		log.Printf("PublishVolume call failed: %s\n", f.err.Error())
	}
	f.publishVolumeRequest = nil
	return nil
}

func (f *feature) aValidControllerPublishVolumeResponseIsReturned() error {
	if f.err != nil {
		return errors.New("PublishVolume returned error: " + f.err.Error())
	}
	if f.publishVolumeResponse == nil {
		return errors.New("No PublishVolumeResponse returned")
	}
	for key, value := range f.publishVolumeResponse.PublishContext {
		fmt.Printf("PublishContext %s: %s", key, value)
	}
	return nil
}

func (f *feature) aValidNodeStageVolumeResponseIsReturned() error {
	if f.err != nil {
		return errors.New("NodeStageVolume returned error: " + f.err.Error())
	}
	if f.nodeStageVolumeResponse == nil {
		return errors.New("no NodeStageVolumeResponse is returned")
	}

	return nil
}

func (f *feature) aValidNodeUnstageVolumeResponseIsReturned() error {
	if f.err != nil {
		return errors.New("NodeUnstageVolume returned error: " + f.err.Error())
	}
	if f.nodeUnstageVolumeResponse == nil {
		return errors.New("no NodeUnstageVolumeResponse is returned")
	}
	return nil
}

func (f *feature) iCallNodeUnpublishVolume() error {
	ctx := new(context.Context)
	req := f.nodeUnpublishVolumeRequest
	if req == nil {
		_ = f.getNodeUnpublishVolumeRequest()
		req = f.nodeUnpublishVolumeRequest
	}
	if inducedErrors.badVolumeIdentifier {
		req.VolumeId = "bad volume identifier"
	}
	fmt.Printf("Calling NodePublishVolume\n")

	f.nodeUnpublishVolumeResponse, f.err = f.service.NodeUnpublishVolume(*ctx, req)
	if f.err != nil {
		log.Printf("NodePublishVolume call failed: %s\n", f.err.Error())
		if strings.Contains(f.err.Error(), "Target Path is required") {
			// Rollback for the future calls
			f.nodeUnpublishVolumeRequest.TargetPath = datadir
		}
	}
	if f.nodeUnpublishVolumeResponse != nil {
		err := os.RemoveAll(req.TargetPath)
		if err != nil {
			return nil
		}
		log.Printf("vol id %s\n", f.nodeUnpublishVolumeRequest.VolumeId)
	}
	return nil
}

func (f *feature) aValidNodeUnpublishVolumeResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) getControllerPublishVolumeRequest(accessType, nodeID string) *csi.ControllerPublishVolumeRequest {
	capability := new(csi.VolumeCapability)

	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.MountFlags = make([]string, 0)
	mount := new(csi.VolumeCapability_Mount)
	mount.Mount = mountVolume
	capability.AccessType = mount

	if !inducedErrors.omitAccessMode {
		capability.AccessMode = getAccessMode(accessType)
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.ControllerPublishVolumeRequest)
	if !inducedErrors.noVolumeID {
		if inducedErrors.invalidVolumeID || f.createVolumeResponse == nil {
			req.VolumeId = "000-000"
		} else {
			req.VolumeId = "volume1=_=_=19=_=_=System"
		}
	}
	if !inducedErrors.noNodeID {
		req.NodeId = nodeID
	}
	req.Readonly = false
	if !inducedErrors.omitVolumeCapability {
		req.VolumeCapability = capability
	}
	// add in the context
	attributes := map[string]string{}
	attributes[AccessZoneParam] = f.accessZone
	req.VolumeContext = attributes
	return req
}

func (f *feature) aControllerPublishedVolume() error {
	var err error
	// Make the target directory if required
	_, err = os.Stat(datadir)
	if err != nil {
		err = os.MkdirAll(datadir, 0777)
		if err != nil {
			fmt.Printf("Couldn't make datadir: %s\n", datadir)
		}
	}

	// Make the target file if required
	_, err = os.Stat(datafile)
	if err != nil {
		file, err := os.Create(datafile)
		if err != nil {
			fmt.Printf("Couldn't make datafile: %s\n", datafile)
		} else {
			file.Close()
		}
	}

	// Empty WindowsMounts in gofsutil
	gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]
	return nil
}

func (f *feature) aCapabilityWithVoltypeAccess(voltype, access string) error {
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	switch voltype {
	case "block":
		blockVolume := new(csi.VolumeCapability_BlockVolume)
		block := new(csi.VolumeCapability_Block)
		block.Block = blockVolume
		capability.AccessType = block
	case "mount":
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	fmt.Printf("Access mode '%s'", access)
	switch access {
	case "single-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	f.capabilities = make([]*csi.VolumeCapability, 0)
	f.capabilities = append(f.capabilities, capability)
	f.capability = capability
	f.nodePublishVolumeRequest = nil
	return nil
}

func (f *feature) iCallNodePublishVolume() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.nodePublishVolumeRequest
	if req == nil {
		_ = f.getNodePublishVolumeRequest()
		req = f.nodePublishVolumeRequest
	}
	if inducedErrors.badVolumeIdentifier {
		req.VolumeId = "bad volume identifier"
	}
	fmt.Printf("Calling NodePublishVolume\n")
	_, err := f.service.NodePublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	return nil
}

func (f *feature) getNodePublishVolumeRequest() error {
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = Volume1
	req.Readonly = false
	req.VolumeCapability = f.capability
	mount := f.capability.GetMount()
	if mount != nil {
		req.TargetPath = datadir
	}
	attributes := map[string]string{
		"Name":       req.VolumeId,
		"AccessZone": "",
		"Path":       f.service.opts.Path + "/" + req.VolumeId,
	}
	req.VolumeContext = attributes

	f.nodePublishVolumeRequest = req
	return nil
}

func (f *feature) getNodeUnpublishVolumeRequest() error {
	req := new(csi.NodeUnpublishVolumeRequest)
	req.VolumeId = Volume1
	req.TargetPath = datadir
	f.nodeUnpublishVolumeRequest = req
	return nil
}

func (f *feature) iChangeTheTargetPath() error {
	// Make the target directory if required
	_, err := os.Stat(datadir2)
	if err != nil {
		err = os.MkdirAll(datadir2, 0777)
		if err != nil {
			fmt.Printf("Couldn't make datadir: %s\n", datadir2)
		}
	}

	// Make the target file if required
	_, err = os.Stat(datafile2)
	if err != nil {
		file, err := os.Create(datafile2)
		if err != nil {
			fmt.Printf("Couldn't make datafile: %s\n", datafile2)
		} else {
			file.Close()
		}
	}
	req := f.nodePublishVolumeRequest
	block := f.capability.GetBlock()
	if block != nil {
		req.TargetPath = datafile2
	}
	mount := f.capability.GetMount()
	if mount != nil {
		req.TargetPath = datadir2
	}
	return nil
}

func (f *feature) iMarkRequestReadOnly() error {
	f.nodePublishVolumeRequest.Readonly = true
	return nil
}

func (f *feature) iCallNodeStageVolume(volID string, accessType string) error {

	ctx := new(context.Context)
	req := getTypicalNodeStageVolumeRequest(accessType)
	f.nodeStageVolumeRequest = req

	// a customized volume ID can be specified to overwrite the default one
	if volID != "" {
		req.VolumeId = volID
	}

	f.nodeStageVolumeResponse, f.err = f.service.NodeStageVolume(*ctx, req)
	if f.err != nil {
		log.Printf("NodeStageVolume call failed: %s\n", f.err.Error())
	}

	if f.nodeStageVolumeResponse != nil {
		log.Printf("a NodeStageVolumeResponse has been returned\n")
	}

	return nil
}

func (f *feature) iCallNodeUnstageVolume(volID string) error {
	ctx := new(context.Context)
	req := getTypicalNodeUnstageVolumeRequest(volID)
	f.nodeUnstageVolumeRequest = req
	f.nodeUnstageVolumeResponse, f.err = f.service.NodeUnstageVolume(*ctx, req)
	if f.err != nil {
		log.Printf("NodeUnstageVolume call failed: %s\n", f.err.Error())
	}

	if f.nodeStageVolumeResponse != nil {
		log.Printf("a NodeUnstageVolumeResponse has been returned\n")
	}
	return nil
}

func (f *feature) iCallListVolumesWithMaxEntriesStartingToken(arg1 int, arg2 string) error {
	ctx := new(context.Context)
	req := new(csi.ListVolumesRequest)
	//  The starting token is not valid
	if arg2 == "invalid" {
		stepHandlersErrors.StartingTokenInvalidError = true
	}
	req.MaxEntries = int32(arg1)
	req.StartingToken = arg2
	f.listVolumesResponse, f.err = f.service.ListVolumes(*ctx, req)
	if f.err != nil {
		log.Printf("ListVolumes call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) aValidListVolumesResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	fmt.Printf("The volumes are %v\n", f.listVolumesResponse.Entries)
	fmt.Printf("The next token is '%s'\n", f.listVolumesResponse.NextToken)
	return nil
}

func (f *feature) iCallDeleteSnapshot(snapshotID string) error {
	ctx := new(context.Context)
	req := new(csi.DeleteSnapshotRequest)
	req.SnapshotId = snapshotID
	f.deleteSnapshotRequest = req
	_, err := f.service.DeleteSnapshot(*ctx, f.deleteSnapshotRequest)
	if err != nil {
		log.Printf("DeleteSnapshot call failed: %s\n", err.Error())
		f.err = err
		return nil
	}
	fmt.Printf("Delete snapshot successfully\n")
	return nil
}

func getCreateSnapshotRequest(srcVolumeID, name, isiPath string) *csi.CreateSnapshotRequest {
	req := new(csi.CreateSnapshotRequest)
	req.SourceVolumeId = srcVolumeID
	req.Name = name
	parameters := make(map[string]string)
	if isiPath != "none" {
		parameters[IsiPathParam] = isiPath
	}
	req.Parameters = parameters
	return req
}

func (f *feature) iCallCreateSnapshot(srcVolumeID, name, isiPath string) error {
	ctx := new(context.Context)
	f.createSnapshotRequest = getCreateSnapshotRequest(srcVolumeID, name, isiPath)
	req := f.createSnapshotRequest

	f.createSnapshotResponse, f.err = f.service.CreateSnapshot(*ctx, req)
	if f.err != nil {
		log.Printf("CreateSnapshot call failed: %s\n", f.err.Error())
	}
	if f.createSnapshotResponse != nil {
		log.Printf("snapshot id %s\n", f.createSnapshotResponse.GetSnapshot().SnapshotId)
	}
	return nil
}

func (f *feature) aValidCreateSnapshotResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	f.snapshotIDList = append(f.snapshotIDList, f.createSnapshotResponse.Snapshot.SnapshotId)
	fmt.Printf("created snapshot id %s: source volume id %s, sizeInBytes %d, creation time %s\n",
		f.createSnapshotResponse.Snapshot.SnapshotId,
		f.createSnapshotResponse.Snapshot.SourceVolumeId,
		f.createSnapshotResponse.Snapshot.SizeBytes,
		f.createSnapshotResponse.Snapshot.CreationTime)
	return nil
}

func (f *feature) setVolumeContent(isSnapshotType bool, identity string) *csi.CreateVolumeRequest {
	req := f.createVolumeRequest
	if isSnapshotType {

		req.VolumeContentSource = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Snapshot{
				Snapshot: &csi.VolumeContentSource_SnapshotSource{
					SnapshotId: identity,
				},
			},
		}
	} else {
		req.VolumeContentSource = &csi.VolumeContentSource{
			Type: &csi.VolumeContentSource_Volume{
				Volume: &csi.VolumeContentSource_VolumeSource{
					VolumeId: identity,
				},
			},
		}
	}

	return req
}

func (f *feature) iCallCreateVolumeFromSnapshot(srcSnapshotID, name string) error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	f.createVolumeRequest = req
	req.Name = name
	req = f.setVolumeContent(true, srcSnapshotID)
	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume call failed: '%s'\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("volume name '%s' created\n", name)
	}
	return nil
}

func (f *feature) iCallCreateVolumeFromVolume(srcVolumeName, name string) error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	f.createVolumeRequest = req
	req.Name = name
	req = f.setVolumeContent(false, srcVolumeName)
	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume call failed: '%s'\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("volume name '%s' created\n", name)
	}
	return nil
}

func (f *feature) iCallInitializeRealIsilonService() error {
	f.service.initializeService(context.Background())
	return nil
}

func (f *feature) aIsilonServiceWithParams(user, mode string) error {
	f.checkGoRoutines("start aIsilonService")

	f.err = nil
	f.getPluginInfoResponse = nil
	f.volumeIDList = f.volumeIDList[:0]
	f.snapshotIDList = f.snapshotIDList[:0]

	// configure gofsutil; we use a mock interface
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceBindMountError = false
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceGetMountsError = false
	gofsutil.GOFSMock.InduceDevMountsError = false
	gofsutil.GOFSMock.InduceUnmountError = false
	gofsutil.GOFSMock.InduceFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatType = ""
	gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]

	// set induced errors
	inducedErrors.badVolumeIdentifier = false
	inducedErrors.invalidVolumeID = false
	inducedErrors.noVolumeID = false
	inducedErrors.differentVolumeID = false
	inducedErrors.noNodeName = false
	inducedErrors.noNodeID = false
	inducedErrors.omitVolumeCapability = false
	inducedErrors.omitAccessMode = false

	// initialize volume and export existence status
	stepHandlersErrors.ExportNotFoundError = true
	stepHandlersErrors.VolumeNotExistError = true

	// Get the httptest mock handler. Only set
	// a new server if there isn't one already.
	handler := getHandler()
	// Get or reuse the cached service
	f.getServiceWithParams(user, mode)
	if handler != nil && os.Getenv("CSI_ISILON_ENDPOINT") == "" {
		if f.server == nil {
			f.server = httptest.NewServer(handler)
		}
		log.Printf("server url: %s\n", f.server.URL)
		f.service.opts.EndpointURL = f.server.URL
	} else {
		f.server = nil
	}
	f.service.isiSvc, f.err = f.service.GetIsiService(context.Background())
	f.checkGoRoutines("end aIsilonService")
	f.service.logServiceStats()
	if inducedErrors.noIsiService || inducedErrors.autoProbeNotEnabled {
		f.service.isiSvc = nil
	}
	return nil
}

func (f *feature) getServiceWithParams(user, mode string) *service {
	testControllerHasNoConnection = false
	testNodeHasNoConnection = false
	svc := new(service)
	var opts Opts
	opts.User = user
	opts.Password = "blah"
	opts.Endpoint = "127.0.0.1"
	opts.EndpointURL = "http://127.0.0.1"
	opts.AccessZone = "System"
	opts.Path = "/ifs/data/csi-isilon"
	opts.Insecure = true
	opts.DebugEnabled = true
	opts.Verbose = 1
	if inducedErrors.autoProbeNotEnabled {
		opts.AutoProbe = false
	} else {
		opts.AutoProbe = true
	}
	svc.opts = opts
	svc.mode = mode
	f.service = svc
	f.service.nodeID = "k8s-rhel76-qual-10-247-98-140"
	f.service.nodeIP = "10.247.98.140"
	utils.ConfigureLogger(opts.DebugEnabled)
	return svc
}

func (f *feature) iCallLogStatisticsTimes(times int) error {
	for i := 0; i < times; i++ {
		f.service.logStatistics()
	}
	return nil
}

func (f *feature) iCallBeforeServe() error {
	sp := new(gocsi.StoragePlugin)
	var lis net.Listener
	f.err = f.service.BeforeServe(context.Background(), sp, lis)
	return nil
}

func (f *feature) ICallCreateQuotaInIsiServiceWithNegativeSizeInBytes() error {
	_, f.err = f.service.isiSvc.CreateQuota(f.service.opts.Path, "volume1", -1, true)
	return nil
}

func (f *feature) iCallGetExportRelatedFunctionsInIsiService() error {
	_, f.err = f.service.isiSvc.GetExports()
	_, f.err = f.service.isiSvc.GetExportByIDWithZone(557, "System")
	f.err = f.service.isiSvc.DeleteQuotaByExportIDWithZone("volume1", 557, "System")
	_, _, f.err = f.service.isiSvc.GetExportsWithLimit("2")
	return nil
}

func (f *feature) iCallUnimplementedFunctions() error {
	_, f.err = f.service.ListSnapshots(context.Background(), new(csi.ListSnapshotsRequest))
	_, f.err = f.service.NodeUnpublishVolume(context.Background(), new(csi.NodeUnpublishVolumeRequest))
	_, f.err = f.service.ControllerExpandVolume(context.Background(), new(csi.ControllerExpandVolumeRequest))
	_, f.err = f.service.ControllerUnpublishVolume(context.Background(), new(csi.ControllerUnpublishVolumeRequest))
	_, f.err = f.service.NodeExpandVolume(context.Background(), new(csi.NodeExpandVolumeRequest))
	_, f.err = f.service.NodeGetVolumeStats(context.Background(), new(csi.NodeGetVolumeStatsRequest))
	return nil
}

func (f *feature) iCallInitServiceObject() error {
	service := New()
	if service == nil {
		f.err = errors.New("failed to initialize Service object")
	} else {
		f.err = nil
	}
	return nil
}
