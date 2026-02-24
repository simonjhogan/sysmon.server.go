package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// CPUStats holds the full CPU statistics payload returned as JSON.
type CPUStats struct {
	Timestamp      string         `json:"timestamp"`
	PhysicalCores  int            `json:"physical_cores"`
	LogicalCores   int            `json:"logical_cores"`
	OverallPercent float64        `json:"overall_percent"`
	PerCorePercent []float64      `json:"per_core_percent"`
	LoadAverage    *LoadAvg       `json:"load_average,omitempty"`
	CPUInfo        []CPUInfoEntry `json:"cpu_info,omitempty"`
}

// LoadAvg holds 1, 5 and 15-minute load averages.
type LoadAvg struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// CPUInfoEntry holds descriptive info for one physical CPU.
type CPUInfoEntry struct {
	ModelName string  `json:"model_name"`
	Cores     int32   `json:"cores"`
	Mhz       float64 `json:"mhz"`
	CacheSize int32   `json:"cache_size_kb"`
}

func getCPU(w http.ResponseWriter, r *http.Request) {
	// Measure CPU usage over a short interval (200 ms) for accuracy.
	const sampleInterval = 200 * time.Millisecond

	overallSlice, err := cpu.Percent(sampleInterval, false)
	if err != nil {
		http.Error(w, `{"error":"failed to get overall CPU usage"}`, http.StatusInternalServerError)
		return
	}

	perCoreSlice, err := cpu.Percent(0, true)
	if err != nil {
		http.Error(w, `{"error":"failed to get per-core CPU usage"}`, http.StatusInternalServerError)
		return
	}

	physCores, _ := cpu.Counts(false)
	logCores, _ := cpu.Counts(true)

	var overall float64
	if len(overallSlice) > 0 {
		overall = overallSlice[0]
	}

	// Load averages (Linux/macOS only; silently omitted on Windows).
	var la *LoadAvg
	if avg, err := load.Avg(); err == nil {
		la = &LoadAvg{
			Load1:  avg.Load1,
			Load5:  avg.Load5,
			Load15: avg.Load15,
		}
	}

	// Static CPU info (brand, MHz, cache).
	infoList, _ := cpu.Info()
	var cpuInfoEntries []CPUInfoEntry
	for _, info := range infoList {
		cpuInfoEntries = append(cpuInfoEntries, CPUInfoEntry{
			ModelName: info.ModelName,
			Cores:     info.Cores,
			Mhz:       info.Mhz,
			CacheSize: info.CacheSize,
		})
	}

	stats := CPUStats{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		PhysicalCores:  physCores,
		LogicalCores:   logCores,
		OverallPercent: overall,
		PerCorePercent: perCoreSlice,
		LoadAverage:    la,
		CPUInfo:        cpuInfoEntries,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// MemStats holds the memory statistics payload returned as JSON.
type MemStats struct {
	Timestamp      string  `json:"timestamp"`
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	FreeBytes      uint64  `json:"free_bytes"`
	BuffersBytes   uint64  `json:"buffers_bytes"`
	CachedBytes    uint64  `json:"cached_bytes"`
}

func getMemory(w http.ResponseWriter, r *http.Request) {
	v, err := mem.VirtualMemory()
	if err != nil {
		http.Error(w, `{"error":"failed to get memory statistics"}`, http.StatusInternalServerError)
		return
	}

	stats := MemStats{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		TotalBytes:     v.Total,
		AvailableBytes: v.Available,
		UsedBytes:      v.Used,
		UsedPercent:    v.UsedPercent,
		FreeBytes:      v.Free,
		BuffersBytes:   v.Buffers,
		CachedBytes:    v.Cached,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// SwapStats holds the swap/page-file statistics payload returned as JSON.
type SwapStats struct {
	Timestamp   string  `json:"timestamp"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
	SinBytes    uint64  `json:"sin_bytes"`  // bytes swapped in from disk
	SoutBytes   uint64  `json:"sout_bytes"` // bytes swapped out to disk
}

func getSwap(w http.ResponseWriter, r *http.Request) {
	s, err := mem.SwapMemory()
	if err != nil {
		http.Error(w, `{"error":"failed to get swap statistics"}`, http.StatusInternalServerError)
		return
	}

	stats := SwapStats{
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TotalBytes:  s.Total,
		UsedBytes:   s.Used,
		FreeBytes:   s.Free,
		UsedPercent: s.UsedPercent,
		SinBytes:    s.Sin,
		SoutBytes:   s.Sout,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// DiskPartitionStats holds usage and I/O counters for one mounted partition.
type DiskPartitionStats struct {
	Mountpoint  string  `json:"mountpoint"`
	FSType      string  `json:"fstype"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
	// I/O counters (nil when unavailable)
	ReadsCompleted  *uint64 `json:"reads_completed,omitempty"`
	WritesCompleted *uint64 `json:"writes_completed,omitempty"`
	ReadBytes       *uint64 `json:"read_bytes,omitempty"`
	WriteBytes      *uint64 `json:"write_bytes,omitempty"`
}

// DiskStats is the top-level payload for GET /disk.
type DiskStats struct {
	Timestamp  string               `json:"timestamp"`
	Partitions []DiskPartitionStats `json:"partitions"`
}

func getDisk(w http.ResponseWriter, r *http.Request) {
	parts, err := disk.Partitions(false) // false = physical devices only
	if err != nil {
		http.Error(w, `{"error":"failed to list disk partitions"}`, http.StatusInternalServerError)
		return
	}

	// Gather per-device I/O counters in one syscall.
	ioMap, _ := disk.IOCounters()

	var partitions []DiskPartitionStats
	for _, p := range parts {
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue // skip unmounted / permission-denied paths
		}

		entry := DiskPartitionStats{
			Mountpoint:  p.Mountpoint,
			FSType:      p.Fstype,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: usage.UsedPercent,
		}

		if io, ok := ioMap[p.Device]; ok {
			reads := io.ReadCount
			writes := io.WriteCount
			rb := io.ReadBytes
			wb := io.WriteBytes
			entry.ReadsCompleted = &reads
			entry.WritesCompleted = &writes
			entry.ReadBytes = &rb
			entry.WriteBytes = &wb
		}

		partitions = append(partitions, entry)
	}

	stats := DiskStats{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Partitions: partitions,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// NetworkInterfaceStats holds I/O counters and addresses for one NIC.
type NetworkInterfaceStats struct {
	Name        string   `json:"name"`
	Addresses   []string `json:"addresses"`
	BytesSent   uint64   `json:"bytes_sent"`
	BytesRecv   uint64   `json:"bytes_recv"`
	PacketsSent uint64   `json:"packets_sent"`
	PacketsRecv uint64   `json:"packets_recv"`
	ErrIn       uint64   `json:"errors_in"`
	ErrOut      uint64   `json:"errors_out"`
	DropIn      uint64   `json:"drops_in"`
	DropOut     uint64   `json:"drops_out"`
}

// NetworkStats is the top-level payload for GET /network.
type NetworkStats struct {
	Timestamp  string                  `json:"timestamp"`
	Interfaces []NetworkInterfaceStats `json:"interfaces"`
}

func getNetwork(w http.ResponseWriter, r *http.Request) {
	// Per-interface I/O counters.
	counters, err := psnet.IOCounters(true) // true = per-interface
	if err != nil {
		http.Error(w, `{"error":"failed to get network I/O counters"}`, http.StatusInternalServerError)
		return
	}

	// Interface addresses keyed by interface name.
	ifaceList, _ := psnet.Interfaces()
	addrMap := make(map[string][]string, len(ifaceList))
	for _, iface := range ifaceList {
		for _, addr := range iface.Addrs {
			addrMap[iface.Name] = append(addrMap[iface.Name], addr.Addr)
		}
	}

	var interfaces []NetworkInterfaceStats
	for _, c := range counters {
		interfaces = append(interfaces, NetworkInterfaceStats{
			Name:        c.Name,
			Addresses:   addrMap[c.Name],
			BytesSent:   c.BytesSent,
			BytesRecv:   c.BytesRecv,
			PacketsSent: c.PacketsSent,
			PacketsRecv: c.PacketsRecv,
			ErrIn:       c.Errin,
			ErrOut:      c.Errout,
			DropIn:      c.Dropin,
			DropOut:     c.Dropout,
		})
	}

	stats := NetworkStats{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Interfaces: interfaces,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(stats); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// HostDetails holds descriptive information about the host machine.
type HostDetails struct {
	Timestamp            string     `json:"timestamp"`
	Hostname             string     `json:"hostname"`
	Uptime               uint64     `json:"uptime_seconds"`
	BootTime             uint64     `json:"boot_time_epoch"`
	OS                   string     `json:"os"`
	Platform             string     `json:"platform"`
	PlatformFamily       string     `json:"platform_family"`
	PlatformVersion      string     `json:"platform_version"`
	KernelVersion        string     `json:"kernel_version"`
	KernelArch           string     `json:"kernel_arch"`
	VirtualizationSystem string     `json:"virtualization_system,omitempty"`
	VirtualizationRole   string     `json:"virtualization_role,omitempty"`
	HostID               string     `json:"host_id"`
	Procs                uint64     `json:"procs"`
	Users                []HostUser `json:"users,omitempty"`
}

// HostUser holds information about a currently logged-in user.
type HostUser struct {
	User     string `json:"user"`
	Terminal string `json:"terminal"`
	Host     string `json:"host"`
	Started  int    `json:"started"`
}

func getHost(w http.ResponseWriter, r *http.Request) {
	info, err := host.Info()
	if err != nil {
		http.Error(w, `{"error":"failed to get host information"}`, http.StatusInternalServerError)
		return
	}

	// Logged-in users (best-effort; silently omitted on error).
	var users []HostUser
	if userList, err := host.Users(); err == nil {
		for _, u := range userList {
			users = append(users, HostUser{
				User:     u.User,
				Terminal: u.Terminal,
				Host:     u.Host,
				Started:  u.Started,
			})
		}
	}

	details := HostDetails{
		Timestamp:            time.Now().UTC().Format(time.RFC3339),
		Hostname:             info.Hostname,
		Uptime:               info.Uptime,
		BootTime:             info.BootTime,
		OS:                   info.OS,
		Platform:             info.Platform,
		PlatformFamily:       info.PlatformFamily,
		PlatformVersion:      info.PlatformVersion,
		KernelVersion:        info.KernelVersion,
		KernelArch:           info.KernelArch,
		VirtualizationSystem: info.VirtualizationSystem,
		VirtualizationRole:   info.VirtualizationRole,
		HostID:               info.HostID,
		Procs:                info.Procs,
		Users:                users,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(details); err != nil {
		log.Printf("encode error: %v", err)
	}
}

// ProcessInfo holds details for a single running process.
type ProcessInfo struct {
	PID             int32    `json:"pid"`
	Name            string   `json:"name"`
	Status          []string `json:"status"`
	CPUPercent      float64  `json:"cpu_percent"`
	MemoryPercent   float32  `json:"memory_percent"`
	RSSBytes        uint64   `json:"rss_bytes"`
	VMSBytes        uint64   `json:"vms_bytes"`
	Username        string   `json:"username,omitempty"`
	Cmdline         string   `json:"cmdline,omitempty"`
	Exe             string   `json:"exe,omitempty"`
	NumThreads      int32    `json:"num_threads"`
	CreateTime      int64    `json:"create_time"`
	CreateTimeHuman string   `json:"create_time_human"`
}

// ProcessList is the top-level payload for GET /process.
type ProcessList struct {
	Timestamp string        `json:"timestamp"`
	Count     int           `json:"count"`
	Processes []ProcessInfo `json:"processes"`
}

func getProcessList(w http.ResponseWriter, r *http.Request) {
	procs, err := process.Processes()
	if err != nil {
		http.Error(w, `{"error":"failed to list processes"}`, http.StatusInternalServerError)
		return
	}

	var processes []ProcessInfo
	for _, p := range procs {
		info := ProcessInfo{PID: p.Pid}

		if name, err := p.Name(); err == nil {
			info.Name = name
		}
		if status, err := p.Status(); err == nil {
			info.Status = status
		}
		if cpuPct, err := p.CPUPercent(); err == nil {
			info.CPUPercent = cpuPct
		}
		if memPct, err := p.MemoryPercent(); err == nil {
			info.MemoryPercent = memPct
		}
		if memInfo, err := p.MemoryInfo(); err == nil && memInfo != nil {
			info.RSSBytes = memInfo.RSS
			info.VMSBytes = memInfo.VMS
		}
		if user, err := p.Username(); err == nil {
			info.Username = user
		}
		if cmdline, err := p.Cmdline(); err == nil {
			info.Cmdline = cmdline
		}
		if exe, err := p.Exe(); err == nil {
			info.Exe = exe
		}
		if threads, err := p.NumThreads(); err == nil {
			info.NumThreads = threads
		}
		if ct, err := p.CreateTime(); err == nil {
			info.CreateTime = ct
			info.CreateTimeHuman = time.UnixMilli(ct).UTC().Format(time.RFC3339)
		}

		processes = append(processes, info)
	}

	payload := ProcessList{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Count:     len(processes),
		Processes: processes,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		log.Printf("encode error: %v", err)
	}
}

func main() {
	port := flag.String("port", "8777", "port to listen on")
	flag.Parse()

	// Allow the PORT environment variable to override the default,
	// but an explicit -port flag takes highest priority.
	if envPort := os.Getenv("PORT"); envPort != "" && !isFlagSet("port") {
		*port = envPort
	}

	http.HandleFunc("/cpu", getCPU)
	http.HandleFunc("/memory", getMemory)
	http.HandleFunc("/swap", getSwap)
	http.HandleFunc("/disk", getDisk)
	http.HandleFunc("/network", getNetwork)
	http.HandleFunc("/host", getHost)
	http.HandleFunc("/process", getProcessList)

	addr := fmt.Sprintf(":%s", *port)
	log.Printf("sysmon listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// isFlagSet reports whether a flag was explicitly set on the command line.
func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
