package main

import "testing"

func TestParseTeamsPlist(t *testing.T) {
	// Shape matches `defaults read com.apple.dt.Xcode IDEProvisioningTeams`.
	const sample = `{
    "dev@example.com" =     (
                {
            isFreeProvisioningTeam = 1;
            teamID = ABCD123456;
            teamName = "Example Developer (Personal Team)";
            teamType = "Personal Team";
        }
    );
}`
	teams := parseTeamsPlist(sample)
	if len(teams) != 1 {
		t.Fatalf("got %d teams, want 1: %+v", len(teams), teams)
	}
	if teams[0].ID != "ABCD123456" {
		t.Errorf("id = %q", teams[0].ID)
	}
	if !teams[0].Free {
		t.Error("expected free team")
	}
	if teams[0].Name == "" {
		t.Error("empty name")
	}
}
