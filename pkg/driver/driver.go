package driver

import (

    "os"
    "context"
    "errors"
    //"time"
    "fmt"

    "github.com/container-storage-interface/spec/lib/go/csi"
    "github.com/go-logr/logr"
   	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
    "golang.org/x/sys/unix"

    "github.com/huang195/spire-csi/pkg/procs"
    "github.com/huang195/spire-csi/pkg/cgroups"
)

// Config is the configuration for the driver
type Config struct {
	Log                  logr.Logger
    NodeID               string
	PluginName           string
	WorkloadAPISocketDir string
}

// Driver is the ephemeral-inline CSI driver implementation
type Driver struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedNodeServer

	log                  logr.Logger
    nodeID               string
	pluginName           string
	workloadAPISocketDir string
}

// New creates a new driver with the given config
func New(config Config) (*Driver, error) {
	switch {
    case config.NodeID == "":
		return nil, errors.New("node ID is required")
	case config.WorkloadAPISocketDir == "":
		return nil, errors.New("workload API socket directory is required")
	}
	return &Driver{
		log:                  config.Log,
        nodeID:               config.NodeID,
		pluginName:           config.PluginName,
		workloadAPISocketDir: config.WorkloadAPISocketDir,
	}, nil
}

/////////////////////////////////////////////////////////////////////////////
// Identity Server
/////////////////////////////////////////////////////////////////////////////

func (d *Driver) GetPluginInfo(context.Context, *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{
		Name:          d.pluginName,
	}, nil
}

func (d *Driver) GetPluginCapabilities(context.Context, *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	// Only the Node server is implemented. No other capabilities are available.
	return &csi.GetPluginCapabilitiesResponse{}, nil
}

func (d *Driver) Probe(context.Context, *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

/////////////////////////////////////////////////////////////////////////////
// Node Server implementation
/////////////////////////////////////////////////////////////////////////////

func (d *Driver) NodePublishVolume(_ context.Context, req *csi.NodePublishVolumeRequest) (_ *csi.NodePublishVolumeResponse, err error) {
	ephemeralMode := req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"]

   	podName := req.GetVolumeContext()["csi.storage.k8s.io/pod.name"]
	podNamespace := req.GetVolumeContext()["csi.storage.k8s.io/pod.namespace"]
	podUID := req.GetVolumeContext()["csi.storage.k8s.io/pod.uid"]
	podServiceAccount := req.GetVolumeContext()["csi.storage.k8s.io/serviceAccount.name"]

    log := d.log.WithValues(
		"volumeID", req.VolumeId,
		"targetPath", req.TargetPath,
	)

    log.Info(fmt.Sprintf("podName: %s\n", podName))
    log.Info(fmt.Sprintf("podNamespace: %s\n", podNamespace))
    log.Info(fmt.Sprintf("podUID: %s\n", podUID))
    log.Info(fmt.Sprintf("podServiceAccount: %s\n", podServiceAccount))

    if req.VolumeCapability != nil && req.VolumeCapability.AccessMode != nil {
		log = log.WithValues("access_mode", req.VolumeCapability.AccessMode.Mode)
	}

    defer func() {
		if err != nil {
			log.Error(err, "Failed to publish volume")
		}
	}()

    // Validate request
	switch {
	case req.VolumeId == "":
		return nil, status.Error(codes.InvalidArgument, "request missing required volume id")
	case req.TargetPath == "":
		return nil, status.Error(codes.InvalidArgument, "request missing required target path")
	case req.VolumeCapability == nil:
		return nil, status.Error(codes.InvalidArgument, "request missing required volume capability")
	case req.VolumeCapability.AccessType == nil:
		return nil, status.Error(codes.InvalidArgument, "request missing required volume capability access type")
	case !isVolumeCapabilityPlainMount(req.VolumeCapability):
		return nil, status.Error(codes.InvalidArgument, "request volume capability access type must be a simple mount")
	case req.VolumeCapability.AccessMode == nil:
		return nil, status.Error(codes.InvalidArgument, "request missing required volume capability access mode")
	case isVolumeCapabilityAccessModeReadOnly(req.VolumeCapability.AccessMode):
		return nil, status.Error(codes.InvalidArgument, "request volume capability access mode is not valid")
	case !req.Readonly:
		return nil, status.Error(codes.InvalidArgument, "pod.spec.volumes[].csi.readOnly must be set to 'true'")
	case ephemeralMode != "true":
		return nil, status.Error(codes.InvalidArgument, "only ephemeral volumes are supported")
	}

    // Create the target path (required by CSI interface)
	if err := os.Mkdir(req.TargetPath, 0777); err != nil && !os.IsExist(err) {
		return nil, status.Errorf(codes.Internal, "unable to create target path %q: %v", req.TargetPath, err)
	}

    if err := unix.Mount("tmpfs", req.TargetPath, "tmpfs", 0, ""); err != nil {
		return nil, status.Errorf(codes.Internal, "unable to mount %q: %v", req.TargetPath, err)
	}

    file, err := os.OpenFile(req.TargetPath+"/hello.txt", os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to create a file %q: %v", req.TargetPath+"/hello.txt", err)
    }
    defer file.Close()


    id, err := procs.GetPodSandboxID(podName,podNamespace)
    if err != nil {
        return nil, err
    }

    _, err = file.WriteString(id+"\n")
    if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to write to file %q: %v", req.TargetPath+"/hello.txt", err)
    }

    pid := os.Getpid()
    log.Info(fmt.Sprintf("My process id: %d\n", pid))

    str, _ := cgroups.ReadCgroups(pid)
    _, err = file.WriteString(str)

    ppid := os.Getppid()
    log.Info(fmt.Sprintf("Parent process id: %d\n", ppid))

    peerProcs, err := procs.GetPeerProcs(ppid)
    if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to get peer processes with parent pid %d: %v", ppid, err)
    }

    for _, peerProc := range peerProcs {
        log.Info(fmt.Sprintf("\tPeer process id: %d, cmdline: %v\n", peerProc.Pid, peerProc.Cmdline))

        str, _ := cgroups.ReadCgroups(peerProc.Pid)
        _, err = file.WriteString(str)
    }

    log.Info("Volume published")

    //time.Sleep(60*time.Second)

    return &csi.NodePublishVolumeResponse{}, nil
}

func (d *Driver) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (_ *csi.NodeUnpublishVolumeResponse, err error) {
	log := d.log.WithValues(
		"volumeID", req.VolumeId,
		"targetPath", req.TargetPath,
	)

	defer func() {
		if err != nil {
			log.Error(err, "Failed to unpublish volume")
		}
	}()

	// Validate request
	switch {
	case req.VolumeId == "":
		return nil, status.Error(codes.InvalidArgument, "request missing required volume id")
	case req.TargetPath == "":
		return nil, status.Error(codes.InvalidArgument, "request missing required target path")
	}

    if err := unix.Unmount(req.TargetPath, 0); err != nil {
        return nil, status.Errorf(codes.Internal, "unable to unmount %q: %v", req.TargetPath, err)
    }

	// Check and remove the mount path if present, report an error otherwise
	if err := os.Remove(req.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, status.Errorf(codes.Internal, "unable to remove target path %q: %v", req.TargetPath, err)
	}

	log.Info("Volume unpublished")

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (d *Driver) NodeGetCapabilities(context.Context, *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{}, nil
}

func (d *Driver) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId:            d.nodeID,
		MaxVolumesPerNode: 0,
	}, nil
}

func isVolumeCapabilityPlainMount(volumeCapability *csi.VolumeCapability) bool {
	mount := volumeCapability.GetMount()
	switch {
	case mount == nil:
		return false
	case mount.FsType != "":
		return false
	case len(mount.MountFlags) != 0:
		return false
	}
	return true
}

func isVolumeCapabilityAccessModeReadOnly(accessMode *csi.VolumeCapability_AccessMode) bool {
	return accessMode.Mode == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
}
