package proc

import (
    "fmt"
    "ioutil"
)

type Proc struct {
    Pid     int
    Cmdline string
}

func GetPeerProcs(ppid int) (procs []Proc, err error) {

    var procs []Proc

    procFiles, err := ioutil.ReadDir("/proc")
   	if err != nil {
		return nil, err
	}

    for _, proc := range procFiles {
        if pidNum, err := strconv.Atoi(proc.Name()); err == nil {
        }

        statusFile := fmt.Sprintf("/proc/%d/status", pidNum)
        statusContent, err := ioutil.ReadFile(statusFile)
        if err != nil {
            continue
        }

        for _, line := range strings.Split(string(statusContent), "\n") {
            if strings.HasPrefix(line, "PPid:") {
					fields := strings.Fields(line)
					if len(fields) == 2 {
						childPPID, err := strconv.Atoi(fields[1])
						if err == nil && childPPID == ppid {
							// If the PPID matches, this is a child process
							cmdline, _ := ioutil.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pidNum))
                            p := Proc{Pid: pidNum, Cmdline: cmdline}
                            procs.append(procs. p)
						}
					}
				}
			}
        }
    }

    return procs, nil
}
