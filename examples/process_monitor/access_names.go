package main

import "fmt"

const (
	NameHostCPU        = "host_cpu"
	NameHostMem        = "host_mem"
	NameBtnFind        = "btn_find"
	NameFilter         = "filter"
	NameChkSmoothCPU   = "smooth_cpu"
	NameNoMatches      = "no_matches"
	NameProc           = "proc"
	NameHeaderPID      = "header-pid"
	NameHeaderCPU      = "header-cpu"
	NameHeaderPower    = "header-power"
	NameHeaderRSS      = "header-rss"
	NameHeaderMem      = "header-mem"
	NameHeaderUptime   = "header-uptime"
	NameHeaderUser     = "header-user"
	NameHeaderState    = "header-state"
	NameHeaderThr      = "header-thr"
	NameHeaderName     = "header-name"
	NameDetailPID      = "detail_pid"
	NameBtnPin         = "btn_pin"
	NameBtnKill        = "btn_kill"
	NameBtnKillConfirm = "btn_kill_confirm"
	NameBtnDeselect    = "btn_deselect"
)

func NameCellPID(pid int) string    { return fmt.Sprintf("p%d-pid", pid) }
func NameCellCPU(pid int) string    { return fmt.Sprintf("p%d-cpu", pid) }
func NameCellPower(pid int) string  { return fmt.Sprintf("p%d-power", pid) }
func NameCellRSS(pid int) string    { return fmt.Sprintf("p%d-rss", pid) }
func NameCellMem(pid int) string    { return fmt.Sprintf("p%d-mem", pid) }
func NameCellUptime(pid int) string { return fmt.Sprintf("p%d-uptime", pid) }
func NameCellUser(pid int) string   { return fmt.Sprintf("p%d-user", pid) }
func NameCellState(pid int) string  { return fmt.Sprintf("p%d-state", pid) }
func NameCellThr(pid int) string    { return fmt.Sprintf("p%d-thr", pid) }
func NameCellName(pid int) string   { return fmt.Sprintf("p%d-name", pid) }
