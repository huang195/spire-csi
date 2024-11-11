package workqueue

import(
    "fmt"
    "time"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "sync"
    "context"

    "github.com/go-logr/logr"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/kubernetes"

    "github.com/huang195/spire-csi/pkg/cert"
    "github.com/huang195/spire-csi/pkg/cgroups"
)

const (
    maxTries    =   10
    driverName  =   "csi-identity.spiffe.io"
)

type Work struct {
    podUID      string
    volumeID    string
    dir         string
}

type Workqueue struct {
    workqueue   map[string]chan bool
    log         logr.Logger
}

func New(log logr.Logger) (*Workqueue) {
    return &Workqueue{
        log:            log,
        workqueue:      make(map[string]chan bool),
    }
}

func worker(quit chan bool, work Work, log logr.Logger) {

    var mu sync.Mutex

    log.Info(fmt.Sprintf("worker started for volumeID %v\n", work))

    myCgroupProcsPath, err := cgroups.GetMyCgroupProcsPath()
    if err != nil {
        log.Error(err, "unable to get my own cgroup")
        return
    }

    err = cgroups.CreateFakeCgroup1(work.podUID)
    if err != nil {
        log.Error(err, "unable to create fake cgroups")
        return
    }
    defer func() {
        cgroups.DeleteFakeCgroup1(work.podUID)
    }()

    for {
        select {
        case <- quit:
            log.Info(fmt.Sprintf("worker stopped for work item: %v\n", work))
            return
        default:
            certFile := filepath.Join(work.dir, "svid.0.pem")
            expirationTime, err := cert.GetCertificateExpirationTime(certFile)
            if err != nil {
                log.Error(err, fmt.Sprintf("cannot open certificate file: %s\n", certFile))
                return
            }

            currentTime := time.Now()
            durationUntilExpiration := expirationTime.Sub(currentTime)
            halfwayDuration := durationUntilExpiration / 2

            // try not to loop too frequently
            if halfwayDuration < 30 * time.Second {
                halfwayDuration = 30 * time.Second
            }

            log.Info(fmt.Sprintf("Timer set for halfway to expiration: %v\n", halfwayDuration))

            select {
            case <- quit:
                log.Info(fmt.Sprintf("worker stopped for volumeID %v\n", work))
                return
            case <- time.After(halfwayDuration):
                log.Info(fmt.Sprintf("Timer expired: halfway to certificate expiration reached"))

                // only 1 goroutine can mess with the cgroup at a time
                mu.Lock()

                // enter cgroup
                err = cgroups.EnterCgroup(os.Getpid(), cgroups.GetPodProcsPath1(work.podUID))
                if err != nil {
                    mu.Unlock()
                    log.Error(err, "cannot enter target cgroup")
                    time.Sleep(1 * time.Second)
                    break
                }

                // Get new identities from spire agent
                try := 1
                for ;try <= maxTries; try++ {
                    cmd := exec.Command("/bin/spire-agent", "api", "fetch", "-socketPath", "/spire-agent-socket/spire-agent.sock", "-write", work.dir)
                    _, err:= cmd.CombinedOutput()
                    if err != nil {
                        log.Error(err, "unable to retrieve spire identities. retrying...")
                        time.Sleep(1 * time.Second)
                    } else {
                        break
                    }
                }
                if try > maxTries {
                    log.Error(fmt.Errorf("unable to retrieve spire identities"), "max tries exceeded")
                }

                // go back to our own cgroup
                cgroups.EnterCgroup(os.Getpid(), myCgroupProcsPath)
                mu.Unlock()
            }
        }
    }
}

func (w *Workqueue) Add(podUID, volumeID, dir string) error {
    if _, exists := w.workqueue[podUID]; !exists {
        quit := make(chan bool)
        w.workqueue[podUID] = quit
        work := Work{
            podUID:     podUID,
            volumeID:   volumeID,
            dir:        dir,
        }
        go worker(quit, work, w.log)
        return nil
    }
    return fmt.Errorf(fmt.Sprintf("volumeID already exists: %s\n", volumeID))
}

func (w *Workqueue) Delete(targetPath string) error {

    // podUID can be extracted from targetPath. This would allow us to use podUID as the key to the workqueue
    // instead of volumeID, as sometimes we don't have the volumeID, e.g., in the background thread
    // e.g., /var/lib/kubelet/pods/fe35a4fa-0d82-41f2-818a-c021e3c10fce/volumes/kubernetes.io~csi/csi-identity/mount
    podUID := ""

    re := regexp.MustCompile(`/pods/([^/]+)/volumes/`)
    match := re.FindStringSubmatch(targetPath)
    if len(match) > 1 {
        podUID = match[1]
    } else {
        return fmt.Errorf(fmt.Sprintf("Cannot parse podUID from targetPath: %v", targetPath))
    }

    if c, exists := w.workqueue[podUID]; exists {
        c <- true
        return nil
    }
    return fmt.Errorf(fmt.Sprintf("cannot find podUID (%s) in the workqueue", podUID))
}

func podUsesCSIDriver(pod *corev1.Pod, driverName string) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.CSI != nil && volume.CSI.Driver == driverName {
			return true
		}
	}
	return false
}

func podIsRunning(pod *corev1.Pod) bool {
    return pod.Status.Phase == corev1.PodRunning
}

//TODO: Need a thread to re-initialize the workqueue when we are restarted and to
// deal with cleaning up things
func (w *Workqueue) Background() {
    log := w.log

    log.Info("workqueue background thread started...")

    var config *rest.Config
    config, err := rest.InClusterConfig()
    if err != nil {
		log.Error(err, "Failed to create in-cluster config")
        os.Exit(1)
	}

    clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error(err, "Failed to create Kubernetes client")
        os.Exit(1)
	}

    nodeName := os.Getenv("MY_NODE_NAME")

    for {
        log.Info("workqueue background thread waking up to look for unhandled deployments")
        pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})

        if err != nil {
            log.Error(err, "Failed to list pods")
            os.Exit(1)
        }

        for _, pod := range pods.Items {
            if pod.Spec.NodeName == nodeName && podUsesCSIDriver(&pod, driverName) && podIsRunning(&pod) {
                fmt.Printf(" - %s (Namespace: %s)\n", pod.Name, pod.Namespace)
            }
        }

        time.Sleep(30*time.Second)
    }
}
