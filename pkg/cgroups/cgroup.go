package cgroups

import(
    "fmt"
    "io/ioutil"
)

func ReadCgroups(pid int) (string, error) {
    cgroup, err := ioutil.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
    return string(cgroup), err
}
