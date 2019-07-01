// Copyright 2019 The Swarm Authors
// This file is part of the Swarm library.
//
// The Swarm library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Swarm library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Swarm library. If not, see <http://www.gnu.org/licenses/>.

package streem

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethersphere/swarm/chunk"
	"github.com/ethersphere/swarm/log"
	"github.com/ethersphere/swarm/network"
	"github.com/ethersphere/swarm/network/streem/spec"
	"github.com/ethersphere/swarm/network/timeouts"
	"github.com/ethersphere/swarm/p2p/protocols"
	"github.com/ethersphere/swarm/state"
	"github.com/ethersphere/swarm/storage"
)

var StreemSpec = &protocols.Spec{
	Name:       "bzz-streem",
	Version:    8,
	MaxMsgSize: 10 * 1024 * 1024,
	Messages: []interface{}{
		StreamInfoReq{},
		StreamInfoRes{},
		GetRange{},
		OfferedHashes{},
		ChunkDelivery{},
		WantedHashes{},
	},
}

var _ node.Service = (*Streem)(nil)
var (
	pollTime           = 1 * time.Second
	createStreamsDelay = 50 * time.Millisecond //to avoid a race condition where we send a message to a server that hasnt set up yet
)

// Syncer is the base type that handles all client/server operations on a node
// it is instantiated once per stream protocol instance, that is, it should have
// one instance per node
type BaseStreem struct {
	mtx            sync.RWMutex                 // mutex used for global operations (such as adding or removing peers)
	intervalsStore state.Store                  // refactoring candidate - peer could have delegate functions that point to the interval store functions
	protocolPeers  map[enode.ID]spec.StreamPeer // a map of peers per protocol. key: protocol name, value: map of peers by enode.ID
	netStore       *storage.NetStore
	kad            *network.Kademlia

	newPeerFuncs map[string]func(p2p.Peer) spec.StreamPeer

	spec    *protocols.Spec   //this protocol's spec
	balance protocols.Balance //implements protocols.Balance, for accounting
	prices  protocols.Prices  //implements protocols.Prices, provides prices to accounting

	quit chan struct{} // initiates shutdown sequence
}

func NewBaseStreem(intervalsStore state.Store, kad *network.Kademlia, ns *storage.NetStore) *BaseStreem {
	bs := &BaseStreem{
		intervalsStore: intervalsStore,
		protocolPeers:  make(map[string]map[enode.ID]spec.StreamPeer),
		kad:            kad,
		netStore:       ns,
		newPeerFuncs:   make(map[string]func(*p2p.Peer) spec.StreamPeer),
		quit:           make(chan struct{}),
	}

	bs.spec = StreemSpec

	return bs
}

func (s *baseStreem) registerPeerFunc(subProtocol string, peerFunc func(*p2p.Peer) spec.StreamPeer) error {
	if _, ok := s.newPeerFuncs[subProtocol]; ok {
		return fmt.Errorf("new peer function for subprotocol %s already exists", subProtocol)
	}

	s.newPeerFuncs[subProtocol] = peerFunc
	return nil
}

func (s *BaseStreem) addPeer(p *Peer) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.peers[p.ID()] = p
}

func (s *BaseStreem) removePeer(p *Peer) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, found := s.peers[p.ID()]; found {
		log.Error("removing peer", "id", p.ID())
		delete(s.peers, p.ID())
		p.Left()

	} else {
		log.Warn("peer was marked for removal but not found")
		panic("shouldnt happen")
	}
}

// Run is being dispatched when 2 nodes connect
func (s *BaseStreem) Run(p *p2p.Peer, rw p2p.MsgReadWriter) error {
	peer := protocols.NewPeer(p, rw, s.spec)
	bp := network.NewBzzPeer(peer)

	np := network.NewPeer(bp, s.kad)
	s.kad.On(np)
	defer s.kad.Off(np)
	for protoName, _ := range s.newPeerFuncs {

	}

	return peer.Run(s.HandleMsg)
}

func (s *BaseStreem) Protocols() []p2p.Protocol {
	return []p2p.Protocol{
		{
			Name:    "bzz-streem",
			Version: 1,
			Length:  10 * 1024 * 1024,
			Run:     s.Run,
		},
	}
}

func (r *BaseStreem) APIs() []rpc.API {
	return []rpc.API{
		{
			Namespace: "bzz-streem",
			Version:   "1.0",
			Service:   NewAPI(r),
			Public:    false,
		},
	}
}

// Additional public methods accessible through API for pss
type API struct {
	*BaseStreem
}

func NewAPI(s *BaseStreem) *API {
	return &API{SwarmSyncer: s}
}

func (s *BaseStreem) Start(server *p2p.Server) error {
	log.Info("syncer starting")
	return nil
}

func (s *BaseStreem) Stop() error {
	log.Info("syncer shutting down")
	s.mtx.Lock()
	defer s.mtx.Unlock()
	close(s.quit)
	return nil
}

func (s *BaseStreem) NeedData(ctx context.Context, key []byte) (loaded bool, wait func(context.Context) error) {
	start := time.Now()

	fi, loaded, ok := s.netStore.GetOrCreateFetcher(ctx, key, "syncer")
	if !ok {
		return loaded, nil
	}

	return loaded, func(ctx context.Context) error {
		select {
		case <-fi.Delivered:
			metrics.GetOrRegisterResettingTimer(fmt.Sprintf("fetcher.%s.syncer", fi.CreatedBy), nil).UpdateSince(start)
		case <-time.After(timeouts.SyncerClientWaitTimeout):
			metrics.GetOrRegisterCounter("fetcher.syncer.timeout", nil).Inc(1)
			return fmt.Errorf("chunk not delivered through syncing after %dsec. ref=%s", timeouts.SyncerClientWaitTimeout, fmt.Sprintf("%x", key))
		}
		return nil
	}
}

// GetData retrieves the actual chunk from netstore
func (s *BaseStreem) GetData(ctx context.Context, key []byte) ([]byte, error) {
	ch, err := s.netStore.Store.Get(ctx, chunk.ModeGetSync, storage.Address(key))
	if err != nil {
		return nil, err
	}
	return ch.Data(), nil
}

func ParseStream(stream string) (bin uint, err error) {
	arr := strings.Split(stream, "|")
	b, err := strconv.Atoi(arr[1])
	return uint(b), err
}

func EncodeStream(bin uint) string {
	return fmt.Sprintf("SYNC|%d", bin)
}
