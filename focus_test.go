package shirei

import "testing"

func TestRunFrameTabWithNoFocusableControls(t *testing.T) {
	tests := []struct {
		name      string
		modifiers Modifiers
	}{
		{name: "Tab"},
		{name: "Shift+Tab", modifiers: ModShift},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetInputSession()
			RunFrameFn(func() {}) // establish an empty previous frame

			ui.Host.Input.Modifiers = tt.modifiers
			ui.Host.FrameInput.Key = KeyTab
			RunFrameFn(func() {})

			if ui.nextFocused != nil {
				t.Fatal("Tab with no focusable controls scheduled focus")
			}
		})
	}
}
