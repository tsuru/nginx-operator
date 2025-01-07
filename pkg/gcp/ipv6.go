package gcp

import (
	"context"
	"errors"

	gcpComputeClient "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/googleapi"
)

type GcpClient interface {
	EnsureIPV6(ctx context.Context, name string) error
}

type gcpClientImpl struct {
	project string
}

func NewGcpClient(project string) GcpClient {
	return &gcpClientImpl{
		project: project,
	}
}

func (gc *gcpClientImpl) EnsureIPV6(ctx context.Context, name string) error {
	globalAddrClient, err := gcpComputeClient.NewGlobalAddressesRESTClient(ctx)
	if err != nil {
		return err
	}
	defer globalAddrClient.Close()
	getGlobalAddressData := computepb.GetGlobalAddressRequest{
		Address: name,
		Project: gc.project,
	}
	_, err = globalAddrClient.Get(ctx, &getGlobalAddressData)
	if err == nil {
		return nil
	}

	var googleApiError *googleapi.Error
	if !errors.As(err, &googleApiError) {
		return err
	}
	if googleApiError.Code != 404 {
		return err
	}

	ipVersion := "IPV6"
	insertGlobalAddressData := &computepb.InsertGlobalAddressRequest{
		AddressResource: &computepb.Address{
			IpVersion: &ipVersion,
			Name:      &name,
		},
		Project: gc.project,
	}
	_, err = globalAddrClient.Insert(ctx, insertGlobalAddressData)
	if err != nil {
		return err
	}
	return nil
}
