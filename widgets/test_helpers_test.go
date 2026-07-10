package widgets

import "sync"

// Shared across widget headless tests so each file need not declare its own.
var initFontsOnce sync.Once
