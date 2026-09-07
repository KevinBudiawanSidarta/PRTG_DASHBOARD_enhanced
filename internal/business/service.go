package business

type ServiceImpact struct {
	ServiceID, ServiceName, Criticality string
	DependencyWeight                    float64
}
