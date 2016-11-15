package benchlib

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// See http://man7.org/linux/man-pages/man5/proc.5.html

type Tick uint64

type CPUStat struct {
	// All values are "tick" counts, probably 100Hz but 'getconf CLK_TCK' to be sure.
	User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal, Guest, GuestNice Tick
}

type ProcCPUStat struct {
	User   Tick
	System Tick
}

type MachineInfo struct {
	CPU_ModelName string
	CPU_MHz       float64
	CPU_Cores     int

	Mem_Bytes uint64

	TCP_MaxSynBacklog uint64

	// The contents of /proc/stat overall "cpu"
	CPUStat

	// The contents of /proc/<benchpid>/stat
	ProcCPUStat
}

type procFunc map[string]func(string, *MachineInfo)

var (
	processStartTime time.Time

	processMachineInfo *MachineInfo

	cpuFuncs = procFunc{"processor": func(value string, mi *MachineInfo) {
		if num, err := strconv.Atoi(value); err == nil && mi.CPU_Cores <= num {
			mi.CPU_Cores = num + 1
		}
	},
		"model name": func(value string, mi *MachineInfo) {
			mi.CPU_ModelName = value
		},
		"cpu MHz": func(value string, mi *MachineInfo) {
			if num, err := strconv.ParseFloat(value, 64); err == nil {
				mi.CPU_MHz = num
			}
		}}

	memFuncs = procFunc{"MemTotal": func(value string, mi *MachineInfo) {
		if !strings.HasSuffix(value, " kB") {
			return
		}
		if kb, err := strconv.ParseUint(value[0:len(value)-3], 10, 64); err == nil {
			mi.Mem_Bytes = kb * 1024
		}
	}}

	statFuncs = procFunc{"cpu": func(value string, mi *MachineInfo) {
		ts := readTicks(value, 10)
		mi.CPUStat = CPUStat{ts[0], ts[1], ts[2], ts[3], ts[4], ts[5], ts[6], ts[7], ts[8], ts[9]}
	}}

	processOnce sync.Once
)

func readTicks(value string, minVals int) []Tick {
	fs := strings.Split(value, " ")
	if len(fs) < minVals {
		panic("proc entry didn't have enough fields: " + value)
	}
	var ts []Tick
	for _, s := range fs {
		u64, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			u64 = 0
		}
		ts = append(ts, Tick(u64))
	}
	return ts
}

func initProcess() {
	processMachineInfo = readMachineInfo()
}

func ProcessMachineInfo() *MachineInfo {
	processOnce.Do(initProcess)
	return processMachineInfo
}

func MachineCPUStat() CPUStat {
	var mi MachineInfo
	readProcKeyValues("/proc/stat", &mi, " ", statFuncs)
	return mi.CPUStat
}

func ProcessCPUStat(pid int) ProcCPUStat {
	var mi MachineInfo
	readProcLine(fmt.Sprint("/proc/", pid, "/stat"), &mi, func(mi *MachineInfo, line string) {
		ts := readTicks(line, 15)
		mi.ProcCPUStat.User = ts[13]
		mi.ProcCPUStat.System = ts[14]
	})
	return mi.ProcCPUStat
}

func readProcLine(path string, mi *MachineInfo, f func(mi *MachineInfo, line string)) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		panic("Could not read: " + path + ": " + err.Error())
	}
	ls := strings.SplitN(string(b), "\n", 2)
	if len(ls) != 2 || len(ls[1]) != 0 {
		panic("Expecting just one line: " + path)
	}
	f(mi, ls[0])
}

func readMachineInfo() *MachineInfo {
	var mi MachineInfo
	readProcKeyValues("/proc/cpuinfo", &mi, ":", cpuFuncs)
	readProcKeyValues("/proc/meminfo", &mi, ":", memFuncs)
	readProcFileUint64("/proc/sys/net/ipv4/tcp_max_syn_backlog", &mi.TCP_MaxSynBacklog)
	readProcKeyValues("/proc/stat", &mi, " ", statFuncs)
	return &mi
}

func readProcKeyValues(path string, mi *MachineInfo, sep string, pf procFunc) {
	f, err := os.Open(path)
	if err == nil {
		err = scanProcKeyValues(f, mi, sep, pf)
	}
	if err != nil {
		Print("Could not read ", path, ": ", err)
	}
}

func scanProcKeyValues(f io.Reader, mi *MachineInfo, sep string, pf procFunc) error {
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		kv := strings.SplitN(scanner.Text(), sep, 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if kf, ok := pf[key]; ok {
			kf(val, mi)
		}
	}
	return scanner.Err()
}

func readProcFileUint64(path string, p *uint64) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		Print("Could not read ", path, ": ", err)
	}
	if err := parseProcFileUint64(b, p); err != nil {
		Print("Could not parse in ", path, ": '", string(b), "': ", err)
	}

}

func parseProcFileUint64(b []byte, p *uint64) error {
	s := strings.TrimSpace(string(b))
	if ui, err := strconv.ParseUint(s, 10, 64); err != nil {
		return err
	} else {
		*p = ui
		return nil
	}
}
