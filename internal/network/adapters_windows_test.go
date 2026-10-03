//go:build windows

package network

import "testing"

func TestPhysicalAdapterIndexes(t *testing.T) {
	if _, err := physicalAdapterIndexes(); err != nil {
		t.Fatalf("physicalAdapterIndexes: %v", err)
	}
}
