package wl

import (
	"testing"
)

func TestRunTillSkipsDestroyedProxyEvents(t *testing.T) {
	client, server := testConnPair(t)
	ctx := testContext(client)
	buffer := NewBuffer(ctx)
	buffer.Unregister()
	callback := NewCallback(ctx)
	// A compositor release for a destroyed window buffer can arrive while a
	// subsequent window waits for its import/feedback roundtrip to complete.
	data := append(wireEvent(uint32(buffer.Id()), 0, nil), wireEvent(uint32(callback.Id()), 0, make([]byte, 4))...)
	if _, err := server.Write(data); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := ctx.RunTill(callback); err != nil {
		t.Fatalf("roundtrip interrupted by retired buffer: %v", err)
	}
}
