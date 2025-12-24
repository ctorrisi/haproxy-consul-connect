package stats

import (
	"encoding/csv"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/haproxytech/models/v2"
)

type StatsSocket struct {
	socketPath string
}

func NewStatsSocket(socketPath string) *StatsSocket {
	return &StatsSocket{
		socketPath: socketPath,
	}
}

func (s *StatsSocket) Stats() (models.NativeStats, error) {
	conn, err := net.Dial("unix", s.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to stats socket: %w", err)
	}
	defer conn.Close()

	// Send "show stat" command
	_, err = fmt.Fprintf(conn, "show stat\n")
	if err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	// Read CSV response
	reader := csv.NewReader(conn)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) < 2 {
		return models.NativeStats{}, nil
	}

	// Parse header to build column index map
	headers := records[0]
	colIndex := make(map[string]int)
	for i, header := range headers {
		colIndex[strings.TrimPrefix(header, "# ")] = i
	}

	// Parse all stat records into NativeStat objects
	var nativeStats []*models.NativeStat
	for _, record := range records[1:] {
		if len(record) == 0 {
			continue
		}

		stat := parseStatRecord(record, colIndex)
		if stat != nil {
			nativeStats = append(nativeStats, stat)
		}
	}

	// Wrap in NativeStatsCollection
	collection := &models.NativeStatsCollection{
		RuntimeAPI: s.socketPath,
		Stats:      nativeStats,
	}

	return models.NativeStats{collection}, nil
}

func parseStatRecord(record []string, colIndex map[string]int) *models.NativeStat {
	// Helper to safely get column value
	getCol := func(name string) string {
		if idx, ok := colIndex[name]; ok && idx < len(record) {
			return record[idx]
		}
		return ""
	}

	// Helper to parse int64 pointer
	parseInt64Ptr := func(s string) *int64 {
		if s == "" {
			return nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil
		}
		return &v
	}

	pxname := getCol("pxname")
	svname := getCol("svname")
	typeVal := getCol("type")

	// Determine type string (frontend/backend/server)
	var typeStr string
	switch typeVal {
	case "0":
		typeStr = "frontend"
	case "1":
		typeStr = "backend"
	case "2":
		typeStr = "server"
	default:
		typeStr = "unknown"
	}

	// Build the NativeStatStats object with all stat fields
	statStats := &models.NativeStatStats{
		Act:          parseInt64Ptr(getCol("act")),
		Bck:          parseInt64Ptr(getCol("bck")),
		Bin:          parseInt64Ptr(getCol("bin")),
		Bout:         parseInt64Ptr(getCol("bout")),
		Chkdown:      parseInt64Ptr(getCol("chkdown")),
		Chkfail:      parseInt64Ptr(getCol("chkfail")),
		CliAbrt:      parseInt64Ptr(getCol("cli_abrt")),
		Downtime:     parseInt64Ptr(getCol("downtime")),
		Dreq:         parseInt64Ptr(getCol("dreq")),
		Dresp:        parseInt64Ptr(getCol("dresp")),
		Econ:         parseInt64Ptr(getCol("econ")),
		Ereq:         parseInt64Ptr(getCol("ereq")),
		Eresp:        parseInt64Ptr(getCol("eresp")),
		Hrsp1xx:      parseInt64Ptr(getCol("hrsp_1xx")),
		Hrsp2xx:      parseInt64Ptr(getCol("hrsp_2xx")),
		Hrsp3xx:      parseInt64Ptr(getCol("hrsp_3xx")),
		Hrsp4xx:      parseInt64Ptr(getCol("hrsp_4xx")),
		Hrsp5xx:      parseInt64Ptr(getCol("hrsp_5xx")),
		HrspOther:    parseInt64Ptr(getCol("hrsp_other")),
		Iid:          parseInt64Ptr(getCol("iid")),
		Lastchg:      parseInt64Ptr(getCol("lastchg")),
		Lbtot:        parseInt64Ptr(getCol("lbtot")),
		Pid:          parseInt64Ptr(getCol("pid")),
		Qcur:         parseInt64Ptr(getCol("qcur")),
		Qlimit:       parseInt64Ptr(getCol("qlimit")),
		Qmax:         parseInt64Ptr(getCol("qmax")),
		Rate:         parseInt64Ptr(getCol("rate")),
		RateLim:      parseInt64Ptr(getCol("rate_lim")),
		RateMax:      parseInt64Ptr(getCol("rate_max")),
		ReqRate:      parseInt64Ptr(getCol("req_rate")),
		ReqRateMax:   parseInt64Ptr(getCol("req_rate_max")),
		ReqTot:       parseInt64Ptr(getCol("req_tot")),
		Scur:         parseInt64Ptr(getCol("scur")),
		Smax:         parseInt64Ptr(getCol("smax")),
		ConnRate:     parseInt64Ptr(getCol("conn_rate")),
		ConnRateMax:  parseInt64Ptr(getCol("conn_rate_max")),
		ConnTot:      parseInt64Ptr(getCol("conn_tot")),
		Intercepted:  parseInt64Ptr(getCol("intercepted")),
		Dcon:         parseInt64Ptr(getCol("dcon")),
		Dses:         parseInt64Ptr(getCol("dses")),
		Lastsess:     parseInt64Ptr(getCol("lastsess")),
		Qtime:        parseInt64Ptr(getCol("qtime")),
		Ctime:        parseInt64Ptr(getCol("ctime")),
		Rtime:        parseInt64Ptr(getCol("rtime")),
		CheckDuration: parseInt64Ptr(getCol("check_duration")),
	}

	// Build the NativeStat object
	stat := &models.NativeStat{
		BackendName: pxname,
		Name:        svname,
		Type:        typeStr,
		Stats:       statStats,
	}

	return stat
}
