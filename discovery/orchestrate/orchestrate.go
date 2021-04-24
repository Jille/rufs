package orchestrate

import (
	"log"
	"math"

	"github.com/sgielen/rufs/intervals"
	pb "github.com/sgielen/rufs/proto"
)

const MAX_TRANSFERS_FOR_READAHEAD = 2

func overlap(lista []string, listb []string) []string {
	var res []string
	for _, a := range lista {
		for _, b := range listb {
			if a == b {
				res = append(res, a)
				break
			}
		}
	}
	return res
}

func listIsEqual(lista []string, listb []string) bool {
	if len(lista) != len(listb) {
		return false
	}
	for i := 0; i < len(lista); i += 1 {
		if lista[i] != listb[i] {
			return false
		}
	}
	return true
}

type Orchestrator struct {
	// peer -> (have, readnow, readahead)
	ranges map[string]*pb.OrchestrateRequest_UpdateByteRanges
	// peer -> connected to which other peers
	connections map[string]*pb.OrchestrateRequest_ConnectedPeers
	// peer -> sending to which other peers
	transfers map[string][]*pb.OrchestrateResponse_UploadCommand
}

func New() *Orchestrator {
	return &Orchestrator{
		ranges:      map[string]*pb.OrchestrateRequest_UpdateByteRanges{},
		connections: map[string]*pb.OrchestrateRequest_ConnectedPeers{},
		transfers:   map[string][]*pb.OrchestrateResponse_UploadCommand{},
	}
}

func (o *Orchestrator) UpdateByteRanges(peer string, ranges *pb.OrchestrateRequest_UpdateByteRanges) {
	o.ranges[peer] = ranges

	// Remove any transfers that are now in ranges.have
	have := intervals.Intervals{}
	for _, r := range ranges.Have {
		have.Add(r.Start, r.End)
	}

	for sender, transfers := range o.transfers {
		var newTransfers []*pb.OrchestrateResponse_UploadCommand
		for _, transfer := range transfers {
			uncovered := have.FindUncovered(transfer.Range.Start, transfer.Range.End)
			if len(uncovered) == 0 {
				continue
			}
			for _, newTransfer := range uncovered {
				newTransfers = append(newTransfers, &pb.OrchestrateResponse_UploadCommand{
					Peer:  transfer.Peer,
					Range: &pb.Range{Start: newTransfer.Start, End: newTransfer.End},
				})
			}
		}
		o.transfers[sender] = newTransfers
	}
}

func (o *Orchestrator) SetConnectedPeers(peer string, connections *pb.OrchestrateRequest_ConnectedPeers) {
	o.connections[peer] = connections
}

func (o *Orchestrator) UploadFailed(sender string, uf *pb.OrchestrateRequest_UploadFailed) {
	var newTransfers []*pb.OrchestrateResponse_UploadCommand
	for _, receiver := range uf.GetTargetPeers() {
		for _, transfer := range o.transfers[sender] {
			if transfer.Peer != receiver {
				newTransfers = append(newTransfers, transfer)
			}
		}
	}
	o.transfers[sender] = newTransfers
}

func (o *Orchestrator) computeRangeInformation() range_infos {
	rs := range_infos{}
	for _, transfers := range o.transfers {
		for _, transfer := range transfers {
			for _, r := range rs.GetSliceFromRange(transfer.GetRange()) {
				r.Data.PeersReceiving = append(r.Data.PeersReceiving, transfer.GetPeer())
			}
		}
	}

	for peer, peerinfo := range o.ranges {
		for _, have := range peerinfo.GetHave() {
			for _, r := range rs.GetSliceFromRange(have) {
				r.Data.PeersHave = append(r.Data.PeersHave, peer)
			}
		}
		for _, have := range peerinfo.GetReadnow() {
			for _, r := range rs.GetSliceFromRange(have) {
				if !r.Data.PeerIsReceiving(peer) {
					r.Data.PeersReadNow = append(r.Data.PeersReadNow, peer)
				}
			}
		}
		for _, have := range peerinfo.GetReadahead() {
			for _, r := range rs.GetSliceFromRange(have) {
				if !r.Data.PeerIsReceiving(peer) {
					r.Data.PeersReadAhead = append(r.Data.PeersReadAhead, peer)
				}
			}
		}
	}

	return rs
}

func (o *Orchestrator) ComputeNewTransfers() map[string][]*pb.OrchestrateResponse_UploadCommand {
	rs := o.computeRangeInformation()

	type transfer struct {
		isReadAhead bool
		senders     []string
		receiver    string
		bytes       *pb.Range
	}

	var transfers []transfer

	isAdjacentTransfer := func(left, right transfer) bool {
		return left.bytes.End == right.bytes.Start &&
			left.isReadAhead == right.isReadAhead &&
			left.receiver == right.receiver &&
			listIsEqual(left.senders, right.senders)
	}

	addTransfer := func(t transfer) {
		// Try to find if there is already a transfer we can append to
		for i := len(transfers) - 1; i >= 0; i = i - 1 {
			if transfers[i].bytes.End < t.bytes.Start {
				// this transfer is not adjacent to the new one anymore, stop trying
				break
			}
			if isAdjacentTransfer(transfers[i], t) {
				// grow the existing transfer instead of adding a new one
				transfers[i].bytes.End = t.bytes.End
				return
			}
		}
		transfers = append(transfers, t)
	}

	for _, r := range rs.ranges {
		// TODO(sjors): Define priority based on how many peers have this, how many need it now, how many need it later
		for _, receiver := range r.Data.PeersReadNow {
			addTransfer(transfer{
				isReadAhead: false,
				senders:     overlap(r.Data.PeersHave, o.connections[receiver].GetPeers()),
				receiver:    receiver,
				bytes:       &pb.Range{Start: r.Start, End: r.End},
			})
		}

		for _, receiver := range r.Data.PeersReadAhead {
			addTransfer(transfer{
				isReadAhead: true,
				senders:     overlap(r.Data.PeersHave, o.connections[receiver].GetPeers()),
				receiver:    receiver,
				bytes:       &pb.Range{Start: r.Start, End: r.End},
			})
		}
	}

	leastBusySender := func(senders []string) (string, int) {
		res := ""
		lowestBusyness := math.MaxInt32
		for _, sender := range senders {
			busyness := len(o.transfers[sender])
			if busyness < lowestBusyness {
				lowestBusyness = busyness
				res = sender
			}
		}
		return res, lowestBusyness
	}

	res := map[string][]*pb.OrchestrateResponse_UploadCommand{}

	for _, t := range transfers {
		if !t.isReadAhead {
			sender, _ := leastBusySender(t.senders)
			if sender == "" {
				log.Printf("Tried to send block %d-%d to %s, but nobody could send it!", t.bytes.Start, t.bytes.End, t.receiver)
				continue
			}
			command := &pb.OrchestrateResponse_UploadCommand{
				Peer:  t.receiver,
				Range: t.bytes,
			}
			o.transfers[sender] = append(o.transfers[sender], command)
			res[sender] = append(res[sender], command)
		}
	}

	for _, t := range transfers {
		if t.isReadAhead {
			sender, busyness := leastBusySender(t.senders)
			if busyness >= MAX_TRANSFERS_FOR_READAHEAD {
				continue
			}
			if sender == "" {
				log.Printf("Tried to send a block to %s, but nobody could send it!", t.receiver)
				continue
			}
			command := &pb.OrchestrateResponse_UploadCommand{
				Peer:  t.receiver,
				Range: t.bytes,
			}
			o.transfers[sender] = append(o.transfers[sender], command)
			res[sender] = append(res[sender], command)
		}
	}

	return res
}
