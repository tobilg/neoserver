package datasource

import (
	"fmt"

	"github.com/tobilg/neoserver/internal/store"
)

// FactoryFunc is the signature for data source factory functions.
type FactoryFunc func(svc *store.Service) (DataSource, error)

// factoryRegistry holds registered factory functions.
var factoryRegistry = make(map[store.ServiceType]FactoryFunc)

// Register registers a factory function for a service type.
func Register(svcType store.ServiceType, factory FactoryFunc) {
	factoryRegistry[svcType] = factory
}

// CreateFromService creates a DataSource from a store.Service using registered factories.
func CreateFromService(svc *store.Service) (DataSource, error) {
	factory, ok := factoryRegistry[svc.Type]
	if !ok {
		return nil, fmt.Errorf("unsupported service type: %s", svc.Type)
	}
	return factory(svc)
}
