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
	Free bool
}

var (
	reTeamID   = regexp.MustCompile(`teamID\s*=\s*([A-Z0-9]+)\s*;`)
	reTeamName = regexp.MustCompile(`teamName\s*=\s*"([^"]*)"\s*;`)
	reTeamFree = regexp.MustCompile(`isFreeProvisioningTeam\s*=\s*(\d+)\s*;`)
)

func listXcodeTeams() ([]Team, error) {
	out, err := exec.Command("defaults", "read", "com.apple.dt.Xcode", "IDEProvisioningTeams").Output()
	if err != nil {
		return nil, nil
	}
	return parseTeamsPlist(string(out)), nil
}

func parseTeamsPlist(s string) []Team {
	var teams []Team
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

// listCodesignIdentities returns app codesigning identity names from the keychain.
// Installer certificates are not in this set (use listInstallerIdentities).
func listCodesignIdentities() []string {
	return listIdentities("-p", "codesigning")
}

// listAllIdentities returns every valid identity (app + installer, etc.).
func listAllIdentities() []string {
	return listIdentities()
}

// listInstallerIdentities returns package-signing identities for productbuild --sign.
// These do not appear under -p codesigning.
func listInstallerIdentities() []string {
	var out []string
	for _, id := range listAllIdentities() {
		if isMacAppStoreInstallerIdentity(id) || isDeveloperIDInstallerIdentity(id) {
			out = append(out, id)
		}
	}
	return out
}

// isMacAppStoreInstallerIdentity is true for Mac App Store .pkg signing.
func isMacAppStoreInstallerIdentity(id string) bool {
	return strings.Contains(id, "3rd Party Mac Developer Installer") ||
		strings.Contains(id, "Mac Installer Distribution")
}

// isDeveloperIDInstallerIdentity is true for outside-store .pkg signing.
func isDeveloperIDInstallerIdentity(id string) bool {
	return strings.Contains(id, "Developer ID Installer")
}

func listIdentities(securityArgs ...string) []string {
	args := append([]string{"find-identity", "-v"}, securityArgs...)
	out, err := exec.Command("security", args...).Output()
	if err != nil {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(string(out), "\n") {
		m := reIdentityLine.FindStringSubmatch(line)
		if m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}
