package config

type PodRouteConfig struct {
	LocalTunIP           string
	InboundPodTunIP      string
	TrafficManagerRealIP string
	Route                string
}
