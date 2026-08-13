package machine

import "github.com/noopolis/moltnet/pkg/protocol"

// NewDeliveryRegistry constructs the one bounded registry shared by a Session
// and its ProviderExecutor. Callers must inject this same value into both.
func NewDeliveryRegistry(max int) DeliveryRegistry {
	if max <= 0 || max > protocol.MachineMaxDeliveryRegistry {
		max = protocol.MachineMaxDeliveryRegistry
	}
	return newMemoryDeliveryRegistry(max)
}

// DeliveryService records only outcomes that packet 1 cannot infer safely.
// A release is legal solely before SendMessage is invoked or after an explicit
// accepted=false response; every post-send uncertainty is retained by packet 1.
type DeliveryService struct {
	registry DeliveryRegistry
}

func NewDeliveryService(registry DeliveryRegistry) *DeliveryService {
	return &DeliveryService{registry: registry}
}

func (service *DeliveryService) release(request protocol.MachineRequest) {
	if service == nil || service.registry == nil {
		return
	}
	identity, ok := (DeliveryIdentityFromSendNudge{}).Identity(request)
	if ok {
		service.registry.Release(identity)
	}
}
