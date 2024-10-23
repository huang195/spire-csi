package workqueue

import(
    "fmt"
    "time"

    "github.com/go-logr/logr"
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
            time.Sleep(60 * time.Second)           
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
