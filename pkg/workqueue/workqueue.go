package workqueue

import(
    "fmt"
    "time"
    "os/exec"

    "github.com/go-logr/logr"

    "github.com/huang195/spire-csi/pkg/cert"
)

const (
    maxTries    =   10
)

type Work struct {
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
    log.Info(fmt.Sprintf("worker started for volumeID %v\n", work))
    for {
        select {
        case <- quit:
            log.Info(fmt.Sprintf("worker stopped for volumeID %v\n", work))
            return
        default:
            certFile := work.dir+"/svid.0.pem"
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

                // Need to get new identities from spire agent
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
            }
        }
    }
}

func (w *Workqueue) Add(volumeID string, dir string) error {
    if _, exists := w.workqueue[volumeID]; !exists {
        quit := make(chan bool)
        w.workqueue[volumeID] = quit
        work := Work{
            volumeID: volumeID,
            dir: dir,
        }
        go worker(quit, work, w.log)
        return nil
    }
    return fmt.Errorf(fmt.Sprintf("volumeID already exists: %s\n", volumeID))
}

func (w *Workqueue) Delete(volumeID string) error {
    if c, exists := w.workqueue[volumeID]; exists {
        c <- true
        return nil
    }
    return fmt.Errorf(fmt.Sprintf("volumeID does not exist: %s\n", volumeID))
}
