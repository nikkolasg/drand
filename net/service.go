package net

import (
	"context"
	"errors"

	"github.com/drand/drand/protobuf/drand"
)

// Service holds all functionalities that a drand node should implement
type Service interface {
	drand.PublicServer
	drand.ControlServer
	drand.ProtocolServer
}

// DefaultService implements the Service from the net package by returning an
// error on each network call.
type DefaultService struct{}

var _ Service = (*DefaultService)(nil)

var ErrNotImplemented = errors.New("call not implemented")

func (d *DefaultService) PublicRand(context.Context, *drand.PublicRandRequest) (*drand.PublicRandResponse, error) {
	return nil, ErrNotImplemented
}
func (d *DefaultService) PublicRandStream(*drand.PublicRandRequest, drand.Public_PublicRandStreamServer) error {
	return ErrNotImplemented
}
func (d *DefaultService) PrivateRand(context.Context, *drand.PrivateRandRequest) (*drand.PrivateRandResponse, error) {
	return nil, ErrNotImplemented
}
func (d *DefaultService) ChainInfo(context.Context, *drand.ChainInfoRequest) (*drand.ChainInfoPacket, error) {
	return nil, ErrNotImplemented
}
func (d *DefaultService) Home(context.Context, *drand.HomeRequest) (*drand.HomeResponse, error) {
	return nil, ErrNotImplemented
}
