package app

import "net/url"

type UCANServiceConfig struct {
	Services              ExternalServicesConfig
	ProofSetID            uint64
	InsecureDIDResolution bool
	// PLCDirectory is the endpoint used to resolve did:plc DIDs.
	PLCDirectory url.URL
}
