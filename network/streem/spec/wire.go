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

package spec

import (
	"context"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethersphere/swarm/storage"
)

type StreamPeer interface {
	/*

	   AddInterval(start, end uint64, peerStreamKey string) : error
	   HandleMsg(ctx context.Context, msg interface{}) : error
	   Left()
	   NextInterval(peerStreamKey string) : uint64, uint64, error
	   -collectBatch(ctx context.Context, bin uint, from, to uint64) : []byte, uint64, uint64, error
	   -getIntervalsKey(bin uint) : string
	   -getOrCreateInterval(bin uint) : *intervals.Intervals, error
	  	   SealBatch(ruid uint) : chan error
	*/
	ID() enode.ID

	HandleChunkDelivery(context.Context, *ChunkDelivery)
	HandleGetRange(ctx context.Context, msg *GetRange)
	HandleOfferedHashes(ctx context.Context, msg *OfferedHashes)
	HandleStreamInfoReq(ctx context.Context, msg *StreamInfoReq)
	HandleStreamInfoRes(ctx context.Context, msg *StreamInfoRes)
	HandleWantedHashes(ctx context.Context, msg *WantedHashes)
	RequestStreamRange(ctx context.Context, stream string, bin uint, cursor uint64) error
}

// StreamCaps is sent between two nodes to advertise which streams are supported
type StreamCaps struct {
	Caps []string
}

type StreamInfoReq struct {
	Ruid    uint
	Streams []string
}

type StreamInfoRes struct {
	Ruid    uint
	Streams []StreamDescriptor
}

type StreamDescriptor struct {
	Name    string
	Cursor  uint64
	Bounded bool
}

type GetRange struct {
	Ruid      uint
	Stream    string
	From      uint64
	To        *uint64 `rlp:"nil"`
	BatchSize uint
	Roundtrip bool
}

type OfferedHashes struct {
	Ruid      uint
	LastIndex uint
	Hashes    []byte
}

type WantedHashes struct {
	Ruid      uint
	BitVector []byte
}

type ChunkDelivery struct {
	Ruid   uint
	Chunks []DeliveredChunk
}

type DeliveredChunk struct {
	Addr storage.Address //chunk address
	Data []byte          //chunk data
}

type BatchDone struct {
	Ruid uint
	Last uint
}

type StreamState struct {
	Stream  string
	Code    uint16
	Message string
}
