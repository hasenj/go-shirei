package main

import (
	"os/exec"
	"regexp"
	"strings"
)

// Team is one Apple provisioning team from Xcode's account store.
type Team struct {
	ID   string
	Name string
	// Free is true for Personal Team (7-day profiles, limited apps).
	Free bool
}

var (
	reTeamID   = regexp.MustCompile(`teamID\s*=\s*([A-Z0-9]+)\s*;`)
	reTeamName = regexp.MustCompile(`teamName\s*=\s*"([^"]*)"\s*;`)
	reTeamFree = regexp.MustCompile(`isFreeProvisioningTeam\s*=\s*(\d+)\s*;`)
)

// listXcodeTeams reads com.apple.dt.Xcode IDEProvisioningTeams via `defaults`.
// Returns an empty slice (not an error) when Xcode has never signed in.
func listXcodeTeams() ([]Team, error) {
	out, err := exec.Command("defaults", "read", "com.apple.dt.Xcode", "IDEProvisioningTeams").Output()
	if err != nil {
		// Missing key / no Xcode prefs — not fatal.
		return nil, nil
	}
	return parseTeamsPlist(string(out)), nil
}

// parseTeamsPlist walks the nextstep-style dump from `defaults read`.
// Each team is a brace block containing teamID / teamName / isFreeProvisioningTeam.
func parseTeamsPlist(s string) []Team {
	var teams []Team
	// Split on opening braces of team dicts; a bit crude but stable for this key.
	parts := strings.Split(s, "{")
	for _, part := range parts {
		id := reTeamID.FindStringSubmatch(part)
		if id == nil {
			continue
		}
		t := Team{ID: id[1]}
		if m := reTeamName.FindStringSubmatch(part); m != nil {
			t.Name = m[1]
		}
		if m := reTeamFree.FindStringSubmatch(part); m != nil && m[1] == "1" {
			t.Free = true
		}
		// De-dupe by team ID (same team can appear under multiple Apple IDs).
		dup := false
		for _, e := range teams {
			if e.ID == t.ID {
				dup = true
				break
			}
		}
		if !dup {
			teams = append(teams, t)
		}
	}
	return teams
}

func teamLabel(t Team) string {
	name := t.Name
	if name == "" {
		name = t.ID
	}
	if t.Free {
		return name + " (" + t.ID + ", free)"
	}
	return name + " (" + t.ID + ")"
}
