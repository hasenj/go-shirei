package main

import (
	"math"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"
)

func startDriveProcessMonitor(t *testing.T) int {
	t.Helper()
	return drive.Start(t, ".")
}

func query(t *testing.T, port int, q string) []shirei.AccessNode {
	t.Helper()
	res, err := drive.Query(port, q)
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return res.Nodes
}

func count(t *testing.T, port int, q string) int {
	t.Helper()
	n, err := drive.Count(port, q)
	if err != nil {
		t.Fatalf("count %q: %v", q, err)
	}
	return n
}

func mustCount(t *testing.T, port int, q string, want int) {
	t.Helper()
	if err := drive.WaitCount(port, q, want); err != nil {
		t.Fatal(err)
	}
}

func shot(t *testing.T, port int, name string) {
	t.Helper()
	if err := drive.Shot(port, name); err != nil {
		t.Fatal("shot:", err)
	}
}

func show(t *testing.T, port int, q string) shirei.AccessNode {
	t.Helper()
	n, err := drive.Show(port, q)
	if err != nil {
		t.Fatalf("show %q: %v", q, err)
	}
	return n
}

// waitSample waits for the collector, not the UI: first snapshot is an OS read.
func waitSample(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if count(t, port, NameHostCPU) == 1 && count(t, port, NameProc) >= 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("first process sample did not land")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestDriveHostAndProcessList(t *testing.T) {
	port := startDriveProcessMonitor(t)
	drive.Comment("Wait for the first process sample")
	waitSample(t, port)
	scopeHeight := show(t, port, "process_scope").Rect.Size[1]
	viewHeight := show(t, port, "process_view").Rect.Size[1]
	if math.Abs(float64(scopeHeight-viewHeight)) > .1 {
		t.Fatalf("toolbar heights differ: scope=%v, view=%v", scopeHeight, viewHeight)
	}
	drive.Comment("Check host stats and named table cells")
	mustCount(t, port, NameHostCPU, 1)
	mustCount(t, port, NameHostMem, 1)
	mustCount(t, port, NameHeaderPID, 1)
	mustCount(t, port, NameHeaderCPU, 1)
	mustCount(t, port, NameHeaderName, 1)
	if count(t, port, NameProc) < 1 {
		t.Fatal("no process rows")
	}
	var pid int
	for _, n := range query(t, port, NameProc) {
		if n.Value != "" && n.Rect.Size[1] > 1 {
			pid = procPID(n)
			break
		}
	}
	if pid == 0 {
		t.Fatal("no visible proc")
	}
	mustCount(t, port, NameCellPID(pid), 1)
	mustCount(t, port, NameCellCPU(pid), 1)
	mustCount(t, port, NameCellName(pid), 1)
	shot(t, port, "Process list")
}

func TestDriveFindNoMatches(t *testing.T) {
	port := startDriveProcessMonitor(t)
	waitSample(t, port)
	drive.Comment("Open the filter text field")
	if _, err := drive.ClickOne(port, NameBtnFind); err != nil {
		t.Fatal("click find:", err)
	}
	drive.Comment("Type a filter that matches nothing")
	if err := drive.Type(port, NameFilter, "!@#"); err != nil {
		t.Fatal("type filter:", err)
	}
	mustCount(t, port, NameNoMatches, 1)
	mustCount(t, port, "clear_filter", 1)
	filterRect := show(t, port, NameFilter).Rect
	clearRect := show(t, port, "clear_filter").Rect
	if clearRect.Origin[0] < filterRect.Origin[0] || clearRect.Origin[0]+clearRect.Size[0] > filterRect.Origin[0]+filterRect.Size[0] {
		t.Fatalf("clear button is outside filter: filter=%v clear=%v", filterRect, clearRect)
	}
	shot(t, port, "No matching processes")
	if _, err := drive.ClickOne(port, "clear_filter"); err != nil {
		t.Fatal("clear filter:", err)
	}
	if got := show(t, port, NameFilter).Value; got != "" {
		t.Fatalf("filter after clear = %q", got)
	}
	mustCount(t, port, "clear_filter", 0)
}

func TestDrivePauseResume(t *testing.T) {
	port := startDriveProcessMonitor(t)
	waitSample(t, port)
	mustCount(t, port, NameFilter, 1)

	drive.Comment("Pause holds the sampled values while the UI remains interactive")
	if _, err := drive.ClickOne(port, "pause_sampling"); err != nil {
		t.Fatal(err)
	}
	if !show(t, port, "pause_sampling").Checked {
		t.Fatal("pause button does not reflect paused state")
	}
	stamp := show(t, port, "sample_time").Value
	time.Sleep(1500 * time.Millisecond)
	if got := show(t, port, "sample_time").Value; got != stamp {
		t.Fatalf("sample changes while paused: %s -> %s", stamp, got)
	}
	if err := drive.Type(port, NameFilter, "no matching process"); err != nil {
		t.Fatal(err)
	}
	mustCount(t, port, NameNoMatches, 1)
	if err := drive.Key(port, "space"); err != nil {
		t.Fatal(err)
	}
	if !show(t, port, "pause_sampling").Checked {
		t.Fatal("space in the filter resumes sampling")
	}
	if err := drive.Key(port, "escape"); err != nil {
		t.Fatal(err)
	}
	mustCount(t, port, NameNoMatches, 0)

	drive.Comment("Space outside the filter resumes sampling")
	if err := drive.Key(port, "space"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for show(t, port, "sample_time").Value == stamp {
		if time.Now().After(deadline) {
			t.Fatal("sampling does not resume")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if show(t, port, "pause_sampling").Checked {
		t.Fatal("pause button remains checked after resume")
	}
}

func procPID(n shirei.AccessNode) int {
	pid, _ := strconv.Atoi(n.Value)
	return pid
}

func pidsAscending(rows []shirei.AccessNode) bool {
	if len(rows) < 2 {
		return false
	}
	for i := 1; i < len(rows); i++ {
		a, b := procPID(rows[i-1]), procPID(rows[i])
		if a == 0 || b == 0 || b < a {
			return false
		}
	}
	return true
}

func sortByPID(t *testing.T, port int, min int) []shirei.AccessNode {
	t.Helper()
	waitSample(t, port)
	if len(query(t, port, NameProc)) < min {
		t.Fatalf("query proc: got %d want >= %d", len(query(t, port, NameProc)), min)
	}
	if _, err := drive.ClickOne(port, NameHeaderPID); err != nil {
		t.Fatal("click PID header:", err)
	}
	drive.Frames(2)
	rows := query(t, port, NameProc)
	if len(rows) < min || !pidsAscending(rows) {
		t.Fatalf("PID sort: pids %v", procValues(rows))
	}
	return rows
}

func clickProcPID(t *testing.T, port int, pid int) {
	t.Helper()
	if _, err := drive.ClickOne(port, NameCellPID(pid)); err != nil {
		t.Fatal("click pid cell:", err)
	}
}

func TestDrivePinSortsFirst(t *testing.T) {
	port := startDriveProcessMonitor(t)
	drive.Comment("Sort the table by PID")
	rows := sortByPID(t, port, 3)
	mid := rows[len(rows)/2]
	pid := mid.Value
	if pid == "" || pid == rows[0].Value {
		t.Fatalf("need a mid row below the top, got top=%q mid=%q n=%d", rows[0].Value, pid, len(rows))
	}
	drive.Comment("Select a process in the middle of the list")
	clickProcPID(t, port, procPID(mid))
	drive.Comment("Pin the selected process")
	if _, err := drive.ClickOne(port, NameBtnPin); err != nil {
		t.Fatal("click pin:", err)
	}
	drive.Frames(2)
	rows = query(t, port, NameProc)
	if len(rows) == 0 || rows[0].Value != pid {
		top := ""
		if len(rows) > 0 {
			top = rows[0].Value
		}
		t.Fatalf("after pin: top pid %q want %q", top, pid)
	}
	shot(t, port, "Pinned process at the top")
}

func procValues(rows []shirei.AccessNode) []string {
	out := make([]string, len(rows))
	for i, n := range rows {
		out[i] = n.Value
	}
	return out
}

func startDummyProcess(t *testing.T) (pid int, done <-chan error) {
	t.Helper()
	cmd := exec.Command("sleep", "3600")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("timeout", "/t", "3600", "/nobreak")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 1)
	go func() { ch <- cmd.Wait() }()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd.Process.Pid, ch
}

// waitProc waits for the collector to publish pid (OS sample), not for UI lag.
// Collect is ~1s, so this waits a second before the first query, then every 250ms.
func waitProc(t *testing.T, port int, pid string, ok func(shirei.AccessNode) bool) shirei.AccessNode {
	t.Helper()
	time.Sleep(time.Second)
	deadline := time.Now().Add(3 * time.Second)
	var rows []shirei.AccessNode
	for {
		rows = query(t, port, NameProc)
		for _, n := range rows {
			if n.Value == pid && n.Rect.Size[1] > 1 && ok(n) {
				return n
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("proc pid %s not in list (have %v)", pid, procValues(rows))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func filterToPID(t *testing.T, port int, pid string) {
	t.Helper()
	if _, err := drive.ClickOne(port, NameBtnFind); err != nil {
		t.Fatal("click find:", err)
	}
	if err := drive.Type(port, NameFilter, pid); err != nil {
		t.Fatal("type pid:", err)
	}
}

func confirmKillSelected(t *testing.T, port int) {
	t.Helper()
	if _, err := drive.ClickOne(port, NameBtnKill); err != nil {
		t.Fatal("click kill:", err)
	}
	if _, err := drive.ClickOne(port, NameBtnKillConfirm); err != nil {
		t.Fatal("click confirm kill:", err)
	}
}

func waitDummyExited(t *testing.T, waitCh <-chan error, pid int) {
	t.Helper()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-waitCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("pid %d still running after kill", pid)
	}
}

func TestDriveSelectDeselect(t *testing.T) {
	port := startDriveProcessMonitor(t)
	rows := sortByPID(t, port, 2)
	if len(rows) < 2 {
		t.Fatalf("need two visible rows, got %d", len(rows))
	}
	a, b := rows[0].Value, rows[1].Value
	if a == "" || a == b {
		t.Fatalf("need two distinct pids, got %q %q", a, b)
	}
	drive.Comment("Select the first process")
	clickProcPID(t, port, procPID(rows[0]))
	mustCount(t, port, NameDetailPID, 1)
	shot(t, port, "First process selected")
	if got := show(t, port, NameDetailPID).Value; got != a {
		t.Fatalf("detail_pid: got %q want %q", got, a)
	}
	drive.Comment("Select the second process")
	clickProcPID(t, port, procPID(rows[1]))
	drive.Frames(2)
	if got := show(t, port, NameDetailPID).Value; got != b {
		t.Fatalf("detail_pid: got %q want %q", got, b)
	}
	drive.Comment("Deselect the process")
	if _, err := drive.ClickOne(port, NameBtnDeselect); err != nil {
		t.Fatal("click deselect:", err)
	}
	mustCount(t, port, NameDetailPID, 0)
	mustCount(t, port, NameBtnPin, 0)
	shot(t, port, "Process deselected")
}

func TestDriveKillDummy(t *testing.T) {
	pid, waitCh := startDummyProcess(t)
	pidStr := strconv.Itoa(pid)

	port := startDriveProcessMonitor(t)
	drive.Comment("Wait for the first process sample")
	waitSample(t, port)
	drive.Comment("Filter the list to the dummy process")
	filterToPID(t, port, pidStr)
	waitProc(t, port, pidStr, func(shirei.AccessNode) bool { return true })
	drive.Comment("Select the dummy process")
	clickProcPID(t, port, pid)
	drive.Comment("Kill it")
	confirmKillSelected(t, port)
	waitDummyExited(t, waitCh, pid)
	n := waitProc(t, port, pidStr, func(n shirei.AccessNode) bool { return n.Role == "exited" })
	if n.Role != "exited" {
		t.Fatalf("pid %s role %q, want exited", pidStr, n.Role)
	}
	shot(t, port, "Dummy process exited")
}

func TestDrivePinKillKeepsRow(t *testing.T) {
	pid, waitCh := startDummyProcess(t)
	pidStr := strconv.Itoa(pid)

	port := startDriveProcessMonitor(t)
	drive.Comment("Wait for the first process sample")
	waitSample(t, port)
	drive.Comment("Filter the list to the dummy process")
	filterToPID(t, port, pidStr)
	waitProc(t, port, pidStr, func(shirei.AccessNode) bool { return true })
	drive.Comment("Select and pin it")
	clickProcPID(t, port, pid)
	if _, err := drive.ClickOne(port, NameBtnPin); err != nil {
		t.Fatal("click pin:", err)
	}
	drive.Frames(2)
	if n := show(t, port, NameBtnPin); !n.Checked {
		t.Fatal("btn_pin not checked")
	}
	drive.Comment("Kill the pinned process")
	confirmKillSelected(t, port)
	waitDummyExited(t, waitCh, pid)
	waitProc(t, port, pidStr, func(n shirei.AccessNode) bool { return n.Role == "exited" })
	n := show(t, port, NameBtnPin)
	if !n.Checked {
		t.Fatal("still selected after kill, but pin not checked")
	}
	shot(t, port, "Pinned process still in the list after kill")
}
