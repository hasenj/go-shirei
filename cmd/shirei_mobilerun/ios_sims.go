package main

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
)

type simDeviceType struct {
	Name string `json:"name"`
}

type simRuntime struct {
	Name        string `json:"name"`
	IsAvailable bool   `json:"isAvailable"`
}

func listSimDeviceNames() []string {
	out, err := exec.Command("xcrun", "simctl", "list", "devicetypes", "-j").Output()
	if err != nil {
		return nil
	}
	var data struct {
		DeviceTypes []simDeviceType `json:"devicetypes"`
	}
	if json.Unmarshal(out, &data) != nil {
		return nil
	}
	var names []string
	for _, t := range data.DeviceTypes {
		// Prefer phones for the launcher default list; still include iPads.
		if strings.Contains(t.Name, "iPhone") || strings.Contains(t.Name, "iPad") {
			names = append(names, t.Name)
		}
	}
	sort.Strings(names)
	return names
}

func listSimRuntimeNames() []string {
	out, err := exec.Command("xcrun", "simctl", "list", "runtimes", "-j").Output()
	if err != nil {
		return nil
	}
	var data struct {
		Runtimes []simRuntime `json:"runtimes"`
	}
	if json.Unmarshal(out, &data) != nil {
		return nil
	}
	var names []string
	for _, r := range data.Runtimes {
		if !r.IsAvailable {
			continue
		}
		// "iOS 26.0" — strip build suffix if present; Name is already short.
		if strings.HasPrefix(r.Name, "iOS") {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	return names
}

// runtimeMatchPref returns a value suitable for SHIREI_IOS_RUNTIME (substring).
// Full names like "iOS 26.0" work; "iOS 18" matches any 18.x.
func runtimeMatchPref(name string) string {
	// Prefer major.minor family for preference matching in ios-run.sh:
	// "iOS 26.0" → "iOS 26" so future 26.1 still matches the pref.
	fields := strings.Fields(name)
	if len(fields) >= 2 && fields[0] == "iOS" {
		ver := fields[1]
		if i := strings.IndexByte(ver, '.'); i > 0 {
			// keep major only for broader match? User may want exact.
			// Keep full "iOS 26.0" as listed — ios-run greps with -i.
			return name
		}
		return "iOS " + ver
	}
	return name
}
