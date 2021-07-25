package connectivity

import (
	pb "github.com/sgielen/rufs/proto"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

const BalancerName string = "rufs"

func init() {
	// Use the base implementation of the balancer. It attempts to create a subconn (channel) for each
	// address, allowing us to pick the address to use.
	balancer.Register(base.NewBalancerBuilder(BalancerName, &rufsPickerBuilder{}, base.Config{HealthCheck: true}))
}

type rufsPickerBuilder struct{}

func (*rufsPickerBuilder) Build(info base.PickerBuildInfo) balancer.Picker {
	// Normally, a picker considers all subconns to point to different nodes.
	// In this case, it makes sense to balance requests over them to spread load.
	// In our case, however, we know that they are various connections to the
	// same node. Hence, we try to pick the most efficient connection, preferring
	// TCP over UDP, but we only pre-pick a single subconn. If the set of ready
	// subconns changes, this method will be invoked to pick the new best one.

	// Attempt to use the first ready TCP subconn.
	for subconn, sinfo := range info.ReadySCs {
		endpointType, _ := splitGrpcAddress(sinfo.Address.Addr)
		if endpointType == pb.Endpoint_TCP {
			return &rufsPicker{subConn: subconn}
		}
	}

	// Otherwise, just use any first one.
	for subconn, _ := range info.ReadySCs {
		return &rufsPicker{subConn: subconn}
	}

	// No SCs ready? Return an error picker.
	return base.NewErrPicker(balancer.ErrNoSubConnAvailable)
}

type rufsPicker struct {
	subConn balancer.SubConn
}

func (p *rufsPicker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{SubConn: p.subConn}, nil
}
