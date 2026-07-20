package app

import "net/url"

type UCANServiceConfig struct {
	Services              ExternalServicesConfig
	ProofSetID            uint64
	InsecureDIDResolution bool
	// PLCDirectory is the did:plc directory endpoint used to resolve did:plc
	// DIDs. Nil disables did:plc resolution.
	PLCDirectory *url.URL
}
