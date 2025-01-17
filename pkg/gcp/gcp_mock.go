package gcp

import (
	"context"
	"slices"
)

var _ GcpClient = &MockGcpClient{}

type MockGcpClient struct {
	Adresses []string
}

func (m *MockGcpClient) EnsureIPV6(ctx context.Context, name string) error {
	if slices.Contains(m.Adresses, name) {
		return nil
	}
	m.Adresses = append(m.Adresses, name)
	return nil
}
